// Package hmux provides an HTTP request multiplexer which matches requests to
// handlers using method- and path-based rules.
//
// TODO: more docs
package hmux

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// A Builder constructs a Mux. Rules are added to the Builder by using Handle
// and related helper methods (Get, Post, and so on). After all the rules have
// been added, Build creates the Mux which uses those rules to route incoming
// requests.
type Builder struct {
	matchers []*matcher
}

// NewBuilder creates and returns a new Mux.
func NewBuilder() *Builder {
	return &Builder{}
}

// Get registers a handler for GET requests using the given path pattern.
// Get panics if this rule is the same as a previously registered rule.
func (b *Builder) Get(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodGet, pat, h)
}

// Post registers a handler for POST requests using the given path pattern.
// Post panics if this rule is the same as a previously registered rule.
func (b *Builder) Post(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodPost, pat, h)
}

// Put registers a handler for PUT requests using the given path pattern.
// Put panics if this rule is the same as a previously registered rule.
func (b *Builder) Put(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodPut, pat, h)
}

// Delete registers a handler for DELETE requests using the given path pattern.
// Delete panics if this rule is the same as a previously registered rule.
func (b *Builder) Delete(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodDelete, pat, h)
}

// Head registers a handler for HEAD requests using the given path pattern.
// Head panics if this rule is the same as a previously registered rule.
func (b *Builder) Head(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodHead, pat, h)
}

// Handle registers a handler for the given HTTP method and path pattern.
// If method is the empty string, the handler is registered for all HTTP methods.
// Handle panics if this rule is the same as a previously registered rule.
func (b *Builder) Handle(method, pat string, h http.Handler) {
	if err := b.handle(method, pat, h); err != nil {
		panic("hmux: " + err.Error())
	}
}

var errNilHandler = errors.New("Handle called with nil handler")

func (b *Builder) handle(method, pat string, h http.Handler) error {
	if h == nil {
		return errNilHandler
	}
	p, err := parsePattern(pat)
	if err != nil {
		return err
	}
	i := sort.Search(len(b.matchers), func(i int) bool {
		return !p.less(b.matchers[i].pat)
	})
	if i < len(b.matchers) && !b.matchers[i].pat.less(p) {
		// segs has the same priority as b.matchers[i].segs
		if !b.matchers[i].merge(method, h) {
			return fmt.Errorf("%s %q conflicts with previously registered pattern", method, pat)
		}
		return nil
	}
	ma := &matcher{pat: p}
	if method == "" {
		ma.allMethods = h
	} else {
		ma.addMethodHandler(method, h)
	}
	b.matchers = append(b.matchers, nil)
	copy(b.matchers[i+1:], b.matchers[i:])
	b.matchers[i] = ma
	return nil
}

// Build creates a Mux using the current rules in b. The Mux does not share
// state with b: future changes to b will not affect the built Mux and other
// Muxes may be built from b later (possibly after adding more rules).
func (b *Builder) Build() *Mux {
	m := &Mux{matchers: make([]*matcher, len(b.matchers))}
	for i, ma := range b.matchers {
		m.matchers[i] = ma.clone()
	}
	return m
}

// Mux is an HTTP request multiplexer. It matches the URL path and HTTP method
// of each incoming request to a list of registered rules and calls the handler
// that most closely matches the request. It supplies path-based parameters
// named by the matched rule via the HTTP request context.
//
// A Mux is constructed by adding rules to a Builder. The Mux's rules are static
// once it is built.
type Mux struct {
	matchers []*matcher
}

// ServeHTTP implements the http.Handler interface.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Redirect non-canonical paths.
	if r.Method != http.MethodConnect {
		if r.URL.RawPath == "" {
			if targ, ok := shouldRedirect(r.URL.Path); ok {
				u := *r.URL
				u.Path = targ
				http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
				return
			}
		} else {
			if targ, ok := shouldRedirect(r.URL.RawPath); ok {
				u := *r.URL
				u.RawPath = targ
				u.Path = mustPathUnescape(targ)
				http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
				return
			}
		}
	}

	var opts matchOpts
	pth := r.URL.Path
	if r.URL.RawPath != "" {
		opts |= optReencode
		pth = r.URL.RawPath
	}
	mr := m.handler(r.Method, pth, opts)
	if mr.h == nil {
		if mr.allow != "" {
			w.Header().Set("Allow", mr.allow)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(w, r)
		return
	}
	if mr.p != nil {
		ctx := r.Context()
		if p0 := RequestParams(ctx); p0 != nil {
			p0.merge(mr.p)
			mr.p = p0
		}
		r = r.WithContext(context.WithValue(ctx, paramKey, mr.p))
	}
	mr.h.ServeHTTP(w, r)
}

func shouldRedirect(pth string) (string, bool) {
	if pth == "" {
		return "/", true
	}
	if pth[0] != '/' {
		pth = "/" + pth
	}
	// In the common case, there's no work to do.
	// Optimize for that by scanning for disallowed segments first.
	i := 1
	for i < len(pth) {
		n := strings.IndexByte(pth[i:], '/')
		var seg string
		if n < 0 {
			seg = pth[i:]
			i = len(pth)
		} else {
			seg = pth[i : i+n]
			i += n + 1
		}
		switch seg {
		case "", ".", "..":
			// Need cleaning.
			clean := path.Clean(pth)
			// path.Clean removes the trailing slash.
			if pth[len(pth)-1] == '/' && clean != "/" {
				clean += "/"
			}
			return clean, true
		}
	}
	return pth, false
}

func (m *Mux) handler(method, pth string, opts matchOpts) matchResult {
	var parts []string
	pth, trailingSlash := trimSuffix(pth, "/")
	if trailingSlash {
		opts |= optTrailingSlash
	}
	pth = strings.TrimPrefix(pth, "/")
	if pth != "" {
		parts = strings.Split(pth, "/")
	}
	if opts&optReencode != 0 {
		for i, part := range parts {
			parts[i] = mustPathUnescape(part)
		}
	}
	result := noMatch
	for _, ma := range m.matchers {
		mr := ma.match(method, parts, opts)
		if mr.h != nil {
			return mr
		}
		// Keep the first 405 result we get, if any.
		if result == noMatch {
			result = mr
		}
	}
	return result
}

type segment struct {
	s       string // literal or param name
	isParam bool
	ptyp    paramType // if segParam
}

var (
	errSegmentStar    = errors.New("pattern segment contains a wildcard (*)")
	errEmptyParamName = errors.New("pattern contains a param segment with an empty name")
)

func parseSegment(s string) (segment, error) {
	var seg segment
	// Wildcards are handled separately and the input is not empty.
	if strings.Contains(s, "*") {
		return seg, errSegmentStar
	}
	// FIXME: Document this and explain.
	// (It's needed so we can match literal / in path segments.)
	s, err := url.PathUnescape(s)
	if err != nil {
		return seg, err
	}
	if s[0] != ':' {
		seg.s = s
		return seg, nil
	}
	s = s[1:]
	if s == "" {
		return seg, errEmptyParamName
	}
	seg.isParam = true
	i := strings.IndexByte(s, ':')
	if i < 0 {
		seg.s = s
		seg.ptyp = paramString
		return seg, nil
	}
	if i == 0 {
		return seg, errEmptyParamName
	}
	switch s[i+1:] {
	case "string":
		seg.ptyp = paramString
	case "int32":
		seg.ptyp = paramInt32
	case "int64":
		seg.ptyp = paramInt64
	default:
		return seg, fmt.Errorf("unknown parameter type %q", s[i+1:])
	}
	seg.s = s[:i]
	return seg, nil
}

type pattern struct {
	segs []segment
	// trailingSlash and wildcard are mutually exclusive.
	trailingSlash bool
	wildcard      bool
}

var (
	errPatternWithoutSlash = errors.New("pattern does not begin with a /")
	errPatternSlash        = errors.New("pattern contains //")
)

func parsePattern(pat string) (pattern, error) {
	var p pattern
	if strings.Contains(pat, "//") {
		return p, errPatternSlash
	}
	if !strings.HasPrefix(pat, "/") {
		return p, errPatternWithoutSlash
	}
	pat, p.wildcard = trimSuffix(pat, "/*")
	pat, p.trailingSlash = trimSuffix(pat, "/")
	pat = strings.TrimPrefix(pat, "/")

	// Now:
	// * The pattern doesn't have a //
	// * It doesn't start or end with a /
	// * It might be empty

	params := make(map[string]struct{})
	for pat != "" {
		var part string
		if i := strings.IndexByte(pat, '/'); i >= 0 {
			part, pat = pat[:i], pat[i+1:]
		} else {
			part, pat = pat, ""
		}
		seg, err := parseSegment(part)
		if err != nil {
			return p, err
		}
		if seg.isParam {
			if _, ok := params[seg.s]; ok {
				return p, fmt.Errorf("patterns contains duplicate parameter %q", seg.s)
			}
			params[seg.s] = struct{}{}
		}
		p.segs = append(p.segs, seg)
	}
	return p, nil
}

func (p pattern) less(p1 pattern) bool {
	if len(p.segs) != len(p1.segs) {
		return len(p.segs) < len(p1.segs)
	}
	var end0, end1 int
	if p.wildcard {
		end0 = 1
	}
	if p.trailingSlash {
		end0 = 2
	}
	if p1.wildcard {
		end1 = 1
	}
	if p1.trailingSlash {
		end1 = 2
	}
	if end0 != end1 {
		// trailing slash > wildcard
		return end0 < end1
	}
	for i, seg0 := range p.segs {
		seg1 := p1.segs[i]
		if seg0.isParam != seg1.isParam {
			// literal > param
			return seg0.isParam
		}
		if seg0.isParam {
			if seg0.ptyp != seg1.ptyp {
				return seg0.ptyp < seg1.ptyp
			}
		} else {
			if seg0.s != seg1.s {
				return seg0.s < seg1.s
			}
		}
	}
	return false
}

type matcher struct {
	pat         pattern
	byMethod    map[string]http.Handler
	methodNames []string
	allMethods  http.Handler
}

func (m *matcher) clone() *matcher {
	m1 := *m
	m1.byMethod = make(map[string]http.Handler)
	for k, v := range m.byMethod {
		m1.byMethod[k] = v
	}
	m1.methodNames = append([]string(nil), m.methodNames...)
	return &m1
}

type matchOpts uint8

const (
	optTrailingSlash matchOpts = 1 << iota
	optReencode
)

// A matchResult indicates how a matcher matches (or fails to match) a request.
// There are three possibilities:
//
// 1. If the matcher matches the path and the method, h and p are set.
// 2. If the matcher matches the path but not the method, allow is set to
//    indicate the Allow header in the 405 response.
// 3. If the matcher doesn't match at all, match returns noMatch.
type matchResult struct {
	h     http.Handler
	p     *Params
	allow string
}

var noMatch matchResult

func (m *matcher) match(method string, parts []string, opts matchOpts) matchResult {
	if opts&optTrailingSlash != 0 && !(m.pat.trailingSlash || m.pat.wildcard) {
		return noMatch
	}
	if opts&optTrailingSlash == 0 && m.pat.trailingSlash {
		return noMatch
	}
	if m.pat.wildcard {
		if len(parts) < len(m.pat.segs) {
			return noMatch
		}
	} else {
		if len(parts) != len(m.pat.segs) {
			return noMatch
		}
	}
	var p *Params
	for i, part := range parts {
		if i == len(m.pat.segs) {
			break
		}
		seg := m.pat.segs[i]
		if seg.isParam {
			pr, ok := matchParam(seg, part, opts)
			if !ok {
				return noMatch
			}
			if p == nil {
				p = new(Params)
			}
			p.ps = append(p.ps, pr)
		} else {
			if part != seg.s {
				return noMatch
			}
		}
	}
	if m.pat.wildcard {
		// The pattern "/x/*" should not match requests for "/x".
		// (But it should match "/x/".)
		if len(parts) == len(m.pat.segs) && opts&optTrailingSlash == 0 {
			return noMatch
		}
		if p == nil {
			p = new(Params)
		}
		p.wildcard = "/" + strings.Join(parts[len(m.pat.segs):], "/")
		if opts&optReencode != 0 {
			p.wildcard = mustPathUnescape(p.wildcard)
		}
		p.hasWildcard = true
	}
	if h, ok := m.byMethod[method]; ok {
		return matchResult{h: h, p: p}
	}
	if h := m.allMethods; h != nil {
		return matchResult{h: h, p: p}
	}
	return matchResult{allow: strings.Join(m.methodNames, ", ")}
}

func mustPathUnescape(s string) string {
	s1, err := url.PathUnescape(s)
	if err != nil {
		// This should not happen because these strings come out of
		// previously-parsed URLs.
		panic(err)
	}
	return s1
}

func (m *matcher) merge(method string, h http.Handler) bool {
	if method == "" {
		if m.allMethods != nil {
			return false
		}
		m.allMethods = h
		return true
	}
	return m.addMethodHandler(method, h)
}

func (m *matcher) addMethodHandler(method string, h http.Handler) (added bool) {
	if _, ok := m.byMethod[method]; ok {
		return false
	}
	if m.byMethod == nil {
		m.byMethod = make(map[string]http.Handler)
	}
	m.byMethod[method] = h
	m.methodNames = append(m.methodNames, method)
	sort.Strings(m.methodNames)
	return true
}

type contextKey int

var paramKey contextKey

type paramType int8

const (
	// In precedence order.
	paramString paramType = iota
	paramInt64
	paramInt32
)

func (t paramType) String() string {
	switch t {
	case paramString:
		return "string"
	case paramInt32:
		return "int32"
	case paramInt64:
		return "int64"
	default:
		panic("bad paramType")
	}
}

type param struct {
	name string
	val  string
	n    int64
	typ  paramType
}

func matchParam(seg segment, s string, opts matchOpts) (p param, ok bool) {
	p.name = seg.s
	p.typ = seg.ptyp
	if opts&optReencode == 0 {
		p.val = s
	} else {
		p.val = mustPathUnescape(s)
	}
	switch p.typ {
	case paramString:
	case paramInt32:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return p, false
		}
		p.n = n
	case paramInt64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return p, false
		}
		p.n = n
	}
	return p, true
}

// Params are URL path segments matched by parameters and wildcards given by
// rule patterns registered with a Mux.
type Params struct {
	ps          []param
	wildcard    string
	hasWildcard bool
}

func (p *Params) merge(p1 *Params) {
	if p1.hasWildcard {
		p.wildcard = p1.wildcard
		p.hasWildcard = true
	}
	ps0 := p.ps
outer:
	for _, pp1 := range p1.ps {
		// Override params of the same name from a higher-level mux.
		for i, pp0 := range ps0 {
			if pp0.name == pp1.name {
				p.ps[i] = pp1
				continue outer
			}
		}
		p.ps = append(p.ps, pp1)
	}
}

func (p *Params) get(name string) param {
	for _, pp := range p.ps {
		if pp.name == name {
			return pp
		}
	}
	panic(fmt.Sprintf("hmux: route does not include a parameter named %q", name))
}

// Get returns the value of a named parameter. It panics if p does not include a
// parameter matching the provided name.
//
// For example, if a rule is registered as
//
//   mux.Get("/products/:name", handleProduct)
//
// then the product name may be retrieved inside handleProduct with
//
//   p.Get("name")
//
func (p *Params) Get(name string) string {
	return p.get(name).val
}

// Int returns the value of a named integer-typed parameter as an int.
// It panics if p does not include a parameter matching the provided name
// or if the parameter exists but does not have an integer type.
// If the type of the parameter is int64 and the value is larger than the
// maximum int on the platform, the returned value is truncated (as with any
// int64-to-int conversion).
//
// For example, if a rule is registered as
//
//   mux.Get("/customers/:id:int32", handleCustomer)
//
// then the customer ID may be retrieved as an int inside handleCustomer with
//
//   p.Int("id")
//
func (p *Params) Int(name string) int {
	pp := p.get(name)
	switch pp.typ {
	case paramInt32, paramInt64:
		return int(pp.n)
	default:
		panic(fmt.Sprintf("hmux: parameter %q has non-integer type %s", name, pp.typ))
	}
}

// Int32 returns the value of a named int32-typed parameter.
// It panics if p does not include a parameter matching the provided name
// or if the parameter exists but does not have the int32 type.
//
// For example, if a rule is registered as
//
//   mux.Get("/customers/:id:int32", handleCustomer)
//
// then the customer ID may be retrieved inside handleCustomer with
//
//   p.Int32("id")
//
func (p *Params) Int32(name string) int32 {
	pp := p.get(name)
	if pp.typ != paramInt32 {
		panic(fmt.Sprintf("hmux: parameter %q has type %s, not int32", name, pp.typ))
	}
	return int32(pp.n)
}

// Int64 returns the value of a named integer-typed parameter as an int64.
// It panics if p does not include a parameter matching the provided name
// or if the parameter exists but does not have an integer type.
//
// For example, if a rule is registered as
//
//   mux.Get("/posts/:id:int64", handlePost)
//
// then the post ID may be retrieved inside handlePost with
//
//   p.Int64("id")
//
func (p *Params) Int64(name string) int64 {
	pp := p.get(name)
	switch pp.typ {
	case paramInt32, paramInt64:
		return pp.n
	default:
		panic(fmt.Sprintf("hmux: parameter %q has non-integer type %s", name, pp.typ))
	}
}

// Wildcard returns the path suffix matched by a wildcard rule.
// It panics if p does not contain a wildcard pattern.
//
// For example, if a rule is registered as
//
//   mux.Get("/static/*", handleStatic)
//
// and an incoming GET request for "/static/styles/site.css" matches this rule,
// then p.Wildcard() gives "styles/site.css".
func (p *Params) Wildcard() string {
	if !p.hasWildcard {
		panic("hmux: Wildcard called on params which didn't match a wildcard pattern")
	}
	return p.wildcard
}

// RequestParams retrieves the Params previously registered via matching a Mux
// rule. It returns nil if there are no params in the rule or if ctx does not
// come from an http.Request handled by a Mux.
func RequestParams(ctx context.Context) *Params {
	p, _ := ctx.Value(paramKey).(*Params)
	return p
}

func trimSuffix(s, suf string) (string, bool) {
	s1 := strings.TrimSuffix(s, suf)
	return s1, s1 != s
}
