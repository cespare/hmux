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
	"sort"
	"strconv"
	"strings"
)

// Mux is an HTTP request multiplexer. It matches the URL path and HTTP method
// of each incoming request to a list of registered rules and calls the handler
// that most closely matches the request. It supplies path-based parameters
// named by the matched rule via the HTTP request context.
//
// A Mux has two phases of operation: first, the rules are provided using Handle
// and related helper methods (Get, Post, and so on); second, the mux is used to
// serve HTTP requests. It is not valid to modify the rules once the mux has
// been used for serving HTTP requests and this may lead to data races or
// panics.
type Mux struct {
	matchers []*matcher
}

// New creates and returns a new Mux.
func New() *Mux {
	return &Mux{}
}

// Get registers a handler for GET requests using the given path pattern.
// Get panics if this rule is the same as a previously registered rule.
func (m *Mux) Get(pat string, h http.HandlerFunc) {
	m.Handle(http.MethodGet, pat, h)
}

// Post registers a handler for POST requests using the given path pattern.
// Post panics if this rule is the same as a previously registered rule.
func (m *Mux) Post(pat string, h http.HandlerFunc) {
	m.Handle(http.MethodPost, pat, h)
}

// Put registers a handler for PUT requests using the given path pattern.
// Put panics if this rule is the same as a previously registered rule.
func (m *Mux) Put(pat string, h http.HandlerFunc) {
	m.Handle(http.MethodPut, pat, h)
}

// Delete registers a handler for DELETE requests using the given path pattern.
// Delete panics if this rule is the same as a previously registered rule.
func (m *Mux) Delete(pat string, h http.HandlerFunc) {
	m.Handle(http.MethodDelete, pat, h)
}

// Head registers a handler for HEAD requests using the given path pattern.
// Head panics if this rule is the same as a previously registered rule.
func (m *Mux) Head(pat string, h http.HandlerFunc) {
	m.Handle(http.MethodHead, pat, h)
}

// Handle registers a handler for the given HTTP method and path pattern.
// If method is the empty string, the handler is registered for all HTTP methods.
// Handle panics if this rule is the same as a previously registered rule.
func (m *Mux) Handle(method, pat string, h http.Handler) {
	if err := m.handle(method, pat, h); err != nil {
		panic("hmux: " + err.Error())
	}
}

var errNilHandler = errors.New("Handle called with nil handler")

func (m *Mux) handle(method, pat string, h http.Handler) error {
	if h == nil {
		return errNilHandler
	}
	p, err := parsePattern(pat)
	if err != nil {
		return err
	}
	i := sort.Search(len(m.matchers), func(i int) bool {
		return !p.less(m.matchers[i].pat)
	})
	if i < len(m.matchers) && !m.matchers[i].pat.less(p) {
		// segs has the same priority as m.matchers[i].segs
		if !m.matchers[i].merge(method, h) {
			return fmt.Errorf("%s %q conflicts with previously registered pattern", method, pat)
		}
		return nil
	}
	ma := &matcher{pat: p}
	if method == "" {
		ma.allMethods = h
	} else {
		ma.byMethod = map[string]http.Handler{method: h}
	}
	m.matchers = append(m.matchers, nil)
	copy(m.matchers[i+1:], m.matchers[i:])
	m.matchers[i] = ma
	return nil
}

// ServeHTTP implements the http.Handler interface.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var opts matchOpts
	path := r.URL.Path
	if r.URL.RawPath != "" {
		opts |= optReencode
		path = r.URL.RawPath
	}
	h, p := m.handler(r.Method, path, opts)
	if h == nil {
		http.NotFound(w, r)
		return
	}
	if p != nil {
		ctx := r.Context()
		if p0 := RequestParams(ctx); p0 != nil {
			p0.merge(p)
			p = p0
		}
		r = r.WithContext(context.WithValue(ctx, paramKey, p))
	}
	h.ServeHTTP(w, r)
}

func (m *Mux) handler(method, path string, opts matchOpts) (http.Handler, *Params) {
	var parts []string
	path, trailingSlash := trimSuffix(path, "/")
	if trailingSlash {
		opts |= optTrailingSlash
	}
	path = strings.TrimPrefix(path, "/")
	if path != "" {
		parts = strings.Split(path, "/")
	}
	if opts&optReencode != 0 {
		for i, part := range parts {
			parts[i] = mustPathUnescape(part)
		}
	}
	for _, ma := range m.matchers {
		h, p := ma.match(method, parts, opts)
		if h != nil {
			return h, p
		}
	}
	return nil, nil
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
	pat        pattern
	byMethod   map[string]http.Handler
	allMethods http.Handler
}

type matchOpts uint8

const (
	optTrailingSlash matchOpts = 1 << iota
	optReencode
)

func (m *matcher) match(method string, parts []string, opts matchOpts) (http.Handler, *Params) {
	if opts&optTrailingSlash != 0 && !(m.pat.trailingSlash || m.pat.wildcard) {
		return nil, nil
	}
	if opts&optTrailingSlash == 0 && m.pat.trailingSlash {
		return nil, nil
	}
	if m.pat.wildcard {
		if len(parts) < len(m.pat.segs) {
			return nil, nil
		}
	} else {
		if len(parts) != len(m.pat.segs) {
			return nil, nil
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
				return nil, nil
			}
			if p == nil {
				p = new(Params)
			}
			p.ps = append(p.ps, pr)
		} else {
			if part != seg.s {
				return nil, nil
			}
		}
	}
	if m.pat.wildcard {
		// The pattern "/x/*" should not match requests for "/x".
		// (But it should match "/x/".)
		if len(parts) == len(m.pat.segs) && opts&optTrailingSlash == 0 {
			return nil, nil
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
		return h, p
	}
	if h := m.allMethods; h != nil {
		return h, p
	}
	return nil, nil
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
	if _, ok := m.byMethod[method]; ok {
		return false
	}
	if m.byMethod == nil {
		m.byMethod = make(map[string]http.Handler)
	}
	m.byMethod[method] = h
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
