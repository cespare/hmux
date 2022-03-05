// Package hmux provides an HTTP request multiplexer which matches requests to
// handlers using method- and path-based rules.
//
// Using hmux involves two phases: construction, using a Builder, and request
// serving, using Mux.
//
//   b := hmux.NewBuilder()
//   b.Get("/", handleIndex)
//   ...
//   mux := b.Build()
//   http.ListenAndServe(addr, mux)
//
// Patterns
//
// Builder rules are registered using pattern strings to match URL paths.
//
// A pattern begins with a slash ("/") and contains zero or more segments
// separated by slashes.
//
// In the simplest case, the pattern matches a single route because each segment
// is a literal string:
//
//   b.Get("/home/about", hmux.ServeFile("about.html"))
//
// A pattern segment may instead contain a parameter, which begins with a colon:
//
//   b.Get("/teams/:team/users/:username", serveUser)
//
// This pattern matches many different URL paths:
//
//   /teams/llamas/users/bob
//   /teams/45/users/92
//   ...
//
// A pattern may end with a slash; it only matches URL paths that also end with
// a slash.
//
// A "wildcard" pattern has a segment of "*" at the end (after the final slash):
//
//   b.Get("/lookup/:db/*", handleLookup)
//
// This matches any path beginning with the preceding segments:
//
//   /lookup/miami/a/b/c
//   /lookup/frankfurt/568739
//   /lookup/tokyo/
//   /lookup/
//   (but not /lookup)
//
// Wildcard patterns are especially useful in conjunction with Builder.Prefix
// and Builder.ServeFS, which always treat their patterns as wildcard patterns
// even if they don't have the ending *.
//
// Routing
//
// A Mux routes requests to the handler registered by the most specific rule
// that matches the request's path and method. When comparing two rules,
// the most specific one is the rule with the most specific pattern; if both
// rules have patterns that are equally specific, then the most specific rule is
// the one that matches specific methods rather than all methods.
//
// Pattern specificity is defined as a segment-by-segment comparison,
// starting from the beginning. The types of segments, arranged from most to
// least specific, are:
//
//   * literal ("/a")
//   * int32 parameter ("/:p:int32")
//   * int64 parameter ("/:p:int64")
//   * string parameter ("/:p")
//
// For two patterns having the same segment specificity, a pattern ending with
// slash is more specific than a pattern ending with a wildcard.
//
// As an example, suppose there are five rules:
//
//   b.Get("/x/y", handlerA)
//   b.Get("/x/:p:int32", handlerB)
//   b.Get("/x/:p", handlerC)
//   b.Get("/:p/y", handlerD)
//   b.Handle("", "/x/y", handlerE)
//
// Requests are routed as follows:
//
//   GET /x/y   handlerA
//   GET /x/3   handlerB
//   GET /x/z   handlerC
//   GET /y/z   handlerD
//   POST /x/y  handlerE
//
// If a request matches the patterns of one or more rules but does not match the
// methods of any of those rules, the Mux writes an HTTP 405 ("Method Not
// Allowed") response with an Allow header that lists all of the matching
// methods.
//
// Before routing, if the request path contains any segment that is "" (that is,
// a double slash), ".", or "..", the Mux writes an HTTP 308 redirect to an
// equivalent cleaned path. For example, all of these are redirected to /x/y:
//
//   /x//y
//   /x/./y
//   /x/y/z/..
//
// Parameters
//
// TODO
//
// Param extraction
// Escaping special characters
package hmux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
//
// A Builder is intended to be used at program initialization and, as such, its
// methods panic on incorrect use. In particular, any method that registers a
// pattern (Get, Handle, ServeFile, and so on) panics if the pattern is
// syntactically invalid or if the rule conflicts with any previously registered
// rule.
type Builder struct {
	matchers []*matcher
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Get registers a handler for GET requests using the given path pattern.
func (b *Builder) Get(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodGet, pat, h)
}

// Post registers a handler for POST requests using the given path pattern.
func (b *Builder) Post(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodPost, pat, h)
}

// Put registers a handler for PUT requests using the given path pattern.
func (b *Builder) Put(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodPut, pat, h)
}

// Delete registers a handler for DELETE requests using the given path pattern.
func (b *Builder) Delete(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodDelete, pat, h)
}

// Head registers a handler for HEAD requests using the given path pattern.
func (b *Builder) Head(pat string, h http.HandlerFunc) {
	b.Handle(http.MethodHead, pat, h)
}

// Handle registers a handler for the given HTTP method and path pattern.
// If method is the empty string, the handler is registered for all HTTP methods.
func (b *Builder) Handle(method, pat string, h http.Handler) {
	if err := b.handle(method, pat, h); err != nil {
		panic("hmux: " + err.Error())
	}
}

func (b *Builder) handle(method, pat string, h http.Handler) error {
	if h == nil {
		return errors.New("Handle called with nil handler")
	}
	p, err := parsePattern(pat)
	if err != nil {
		return err
	}
	return b.addHandler(method, pat, p, h)
}

// Prefix registers a handler at the given prefix pattern.
// This is similar to calling Handle with method as "" except that the handler
// is called with a modified request where the matched prefix is removed from
// the beginning of the path.
//
// For example, suppose this method is called as
//
//   b.Prefix("/sub", h)
//
// Then if a request arrives with the path "/sub/x/y", the handler h sees a
// request with a path "/x/y".
//
// Whether pat ends with * or not, Prefix interprets it as a wildcard pattern.
// So the example above would be the same whether the pattern had been given as
// "/sub", "/sub/", or "/sub/*".
func (b *Builder) Prefix(pat string, h http.Handler) {
	if h == nil {
		panic("hmux: Prefix called with nil handler")
	}
	p, err := parsePattern(pat)
	if err != nil {
		panic("hmux: " + err.Error())
	}
	p.end = endWildcard
	ph := prefixHandler{
		h:    h,
		skip: len(p.segs),
	}
	if err := b.addHandler("", pat, p, ph); err != nil {
		panic("hmux: " + err.Error())
	}
}

type prefixHandler struct {
	h    http.Handler
	skip int // how many prefix segments to remove
}

func (h prefixHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r1 := new(http.Request)
	*r1 = *r
	r1.URL = h.trimPrefix(r.URL)
	h.h.ServeHTTP(w, r1)
}

func (h prefixHandler) trimPrefix(u *url.URL) *url.URL {
	u1 := new(url.URL)
	*u1 = *u
	if u.RawPath == "" {
		u1.Path = skipPrefix(u.Path, h.skip)
		return u1
	}
	u1.RawPath = skipPrefix(u.RawPath, h.skip)
	u1.Path = mustPathUnescape(u1.RawPath)
	return u1
}

func skipPrefix(s string, skip int) string {
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	for i := 0; i < skip; i++ {
		j := strings.IndexByte(s[1:], '/')
		if j < 0 {
			panic("skip larger than number of prefix segments")
		}
		s = s[j+1:]
	}
	return s
}

// ServeFile registers GET and HEAD handlers for the given pattern that serve
// the named file using http.ServeFile.
func (b *Builder) ServeFile(pat, name string) {
	if err := b.handleServeFile(pat, name); err != nil {
		panic("hmux: " + err.Error())
	}
}

func (b *Builder) handleServeFile(pat, name string) error {
	p, err := parsePattern(pat)
	if err != nil {
		return err
	}
	var h http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, name)
	}
	if err := b.addHandler(http.MethodGet, pat, p, h); err != nil {
		return err
	}
	if err := b.addHandler(http.MethodHead, pat, p, h); err != nil {
		return err
	}
	return nil
}

// ServeFS serves files from fsys at a prefix pattern.
//
// Like Prefix, the pattern prefix is removed from the beginining of the path
// before lookup in fsys.
func (b *Builder) ServeFS(pat string, fsys fs.FS) {
	b.Prefix(pat, http.FileServer(http.FS(fsys)))
}

func (b *Builder) addHandler(method, pat string, p pattern, h http.Handler) error {
	i := sort.Search(len(b.matchers), func(i int) bool {
		return p.compare(b.matchers[i].pat) >= 0
	})
	if i < len(b.matchers) && b.matchers[i].pat.compare(p) == 0 {
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
// of each incoming request to a list of rules and calls the handler that most
// closely matches the request. It supplies path-based parameters named by the
// matched rule via the HTTP request context.
//
// A Mux is constructed by adding rules to a Builder. The Mux's rules do not
// change after it is built.
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
		} else if targ, ok := shouldRedirect(r.URL.RawPath); ok {
			u := *r.URL
			u.RawPath = targ
			u.Path = mustPathUnescape(targ)
			http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
			return
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
		if p0 := RequestParams(r); p0 != nil {
			p0.merge(mr.p)
			mr.p = p0
		}
		r = r.WithContext(context.WithValue(r.Context(), paramKey, mr.p))
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
	if s[0] != ':' {
		// Unescape the segment because rules are matched against
		// unescaped paths. For example: if we want to match an escaped
		// /, then the rule contains %2f and the request also contains
		// %2f.
		var err error
		seg.s, err = url.PathUnescape(s)
		return seg, err
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
	end  patternEnding
}

type patternEnding int8

const (
	// In precedence order.
	endNone patternEnding = iota
	endWildcard
	endSlash
)

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
	var ok bool
	if pat, ok = trimSuffix(pat, "/*"); ok {
		p.end = endWildcard
	}
	if pat, ok = trimSuffix(pat, "/"); ok {
		p.end = endSlash
	}
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

func (p pattern) compare(p1 pattern) int {
	n := len(p.segs)
	if n > len(p1.segs) {
		n = len(p1.segs)
	}
	for i := 0; i < n; i++ {
		seg0 := p.segs[i]
		seg1 := p1.segs[i]
		if seg0.isParam != seg1.isParam {
			// literal > param
			if seg0.isParam {
				return -1
			} else {
				return 1
			}
		}
		if seg0.isParam {
			if seg0.ptyp != seg1.ptyp {
				return int(seg0.ptyp - seg1.ptyp)
			}
		} else {
			if seg0.s != seg1.s {
				return strings.Compare(seg0.s, seg1.s)
			}
		}
	}
	if len(p.segs) > n {
		return 1
	}
	if len(p1.segs) > n {
		return -1
	}
	return int(p.end - p1.end)
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
	if opts&optTrailingSlash != 0 && m.pat.end == endNone {
		return noMatch
	}
	if opts&optTrailingSlash == 0 && m.pat.end == endSlash {
		return noMatch
	}
	if m.pat.end == endWildcard {
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
	if m.pat.end == endWildcard {
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
		// We shouldn't get here since we only try to merge matchers of
		// the same priority.
		panic("shouldn't happen")
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
// Note that, by construction, a parameter value cannot be empty, so Get never
// returns the empty string.
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
// rule. It returns nil if there are no params in the rule.
func RequestParams(r *http.Request) *Params {
	p, _ := r.Context().Value(paramKey).(*Params)
	return p
}

func trimSuffix(s, suf string) (string, bool) {
	s1 := strings.TrimSuffix(s, suf)
	return s1, s1 != s
}
