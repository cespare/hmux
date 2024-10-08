package hmux

import (
	"fmt"
	"io/fs"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRedirects(t *testing.T) {
	b := NewBuilder()
	b.Get("/abc", testHandler("abc"))
	testCases := []reqTest{
		{"GET", "/x/../abc", "308 /abc"},
		{"GET", "/x/./abc", "308 /x/abc"},
		{"GET", "/a//b/c", "308 /a/b/c"},
		{"GET", "/a/b//c/", "308 /a/b/c/"},
		{"GET", "//a/b/c//", "308 /a/b/c/"},
		{"GET", "//a/b/c//", "308 /a/b/c/"},
		{"GET", "/%2fa//%61/c/", "308 /%2fa/%61/c/"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestSpecialPatterns(t *testing.T) {
	b := NewBuilder()
	b.Handle("", "*", testHandler("star"))
	b.Get("", testHandler("empty"))
	testCases := []reqTest{
		{"OPTIONS", "*", "star"},
		{"GET", "*", "star"},
		{"OPTIONS", "/", "405 GET"},
		{"OPTIONS", "/*", "405 GET"},
		{"GET", "/", "empty"},
		{"GET", "/a/b", "empty"},
	}
	testRequests(t, b.Build(), testCases)
}

// Test the example presented in doc comments.
func TestDocPriorities(t *testing.T) {
	b := NewBuilder()
	b.Get("/x/y", testHandler("A"))
	b.Get("/x/:p:int32", testHandler("B"))
	b.Get("/x/:p", testHandler("C"))
	b.Get("/:p/y", testHandler("D"))
	b.Handle("", "/x/y", testHandler("E"))

	testCases := []reqTest{
		{"GET", "/x/y", "A"},
		{"GET", "/x/3", "B"},
		{"GET", "/x/z", "C"},
		{"GET", "/y/y", "D"},
		{"POST", "/x/y", "E"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestMatchingPriorities(t *testing.T) {
	type testRule struct {
		method string
		pat    string
		h      http.HandlerFunc
	}
	rules := []testRule{
		{"GET", "/", testHandler("index")},
		{"GET", "/x", testHandler("/x")},
		{"POST", "/x", testHandler("post /x")},
		{"GET", "/x/y", testHandler("/x/y")},
		{"GET", "/:p/z", testHandler("/:p/z")},
		{"GET", "/z/y", testHandler("/z/y")},
		{"PUT", "/a/cats/:id", testHandler("put cat %s", "id")},
		{"GET", "/a/cats/6", testHandler("cat 6")},
		{"GET", "/a/cats/xyz", testHandler("cat xyz")},
		{"GET", "/a/cats/:id", testHandler("get cat %s", "id")},
		{"GET", "/:p/cats/xyz", testHandler("%s cat xyz", "p")},
		{"GET", "/:p/cats/:id", testHandler("%s cat id %s", "p", "id")},
		{"GET", "/a/cats/:id:int32", testHandler("get int32 cat %d", "id:int32")},
		{"GET", "/a/cats/:id:int64", testHandler("get int64 cat %d", "id:int64")},
		{"GET", "/a/cats/*", testHandler("get cat wildcard %s", "*")},
		{"GET", "/a/*", testHandler("catch-all %s", "*")},
		{"GET", "/:p/turtles/*", testHandler("%s turtles %s", "p", "*")},
		{"GET", "", testHandler("empty")},
	}

	testCases := []reqTest{
		{"GET", "/", "index"},
		{"POST", "/", "405 GET"},
		{"GET", "/x", "/x"},
		{"PUT", "/x", "405 GET, POST"},
		{"GET", "/x/y", "/x/y"},
		{"GET", "/x/z", "/:p/z"},
		{"GET", "/y", "empty"},
		{"GET", "/z/y", "/z/y"},
		{"POST", "/x", "post /x"},
		{"POST", "/x/y", "405 GET"},
		{"GET", "/a", "empty"},
		{"PUT", "/a/cats/xyz", "put cat xyz"},
		{"GET", "/a/cats/6", "cat 6"},
		{"GET", "/a/cats/xyz", "cat xyz"},
		{"GET", "/b/cats/6", "b cat id 6"},
		{"GET", "/b/cats/xyz", "b cat xyz"},
		{"GET", "/a/cats/123", "get int32 cat 123"},
		{"GET", "/a/cats/123123123123", "get int64 cat 123123123123"},
		{"GET", "/a/cats/123123123123123123123123", "get cat 123123123123123123123123"},
		{"GET", "/a/cats/12x", "get cat 12x"},
		{"GET", "/a/cats/12/3", "get cat wildcard /12/3"},
		{"GET", "/a/cats/a/b/c", "get cat wildcard /a/b/c"},
		{"GET", "/a/dogs/3", "catch-all /dogs/3"},
		{"GET", "/a/turtles/a/b/c", "catch-all /turtles/a/b/c"},
		{"GET", "/b/turtles/a/b/c", "b turtles /a/b/c"},
	}

	for i := 0; i < 200; i++ {
		// Randomize the rule insertion order to flush out differences
		// that result.
		rules1 := rules
		if i > 0 {
			rng := rand.New(rand.NewSource(int64(i)))
			rules1 = make([]testRule, len(rules))
			copy(rules1, rules)
			rng.Shuffle(len(rules1), func(i, j int) {
				rules1[i], rules1[j] = rules1[j], rules1[i]
			})
		}
		b := NewBuilder()
		for _, rule := range rules1 {
			b.Handle(rule.method, rule.pat, rule.h)
		}

		testRequests(t, b.Build(), testCases)

		if t.Failed() {
			if i > 0 {
				t.Logf("test failed with seed=%d", i)
			}
			return
		}
	}
}

func Test405(t *testing.T) {
	b := NewBuilder()
	b.Get("/x", testHandler("get /x"))
	b.Get("/x/y/:name", testHandler("get /x/y/%s", "name"))
	b.Put("/x/y/:name", testHandler("put /x/y/%s", "name"))
	b.Delete("/x/y/:name", testHandler("delete /x/y/%s", "name"))
	b.Handle("MYMETHOD", "/x/y/:name", testHandler("mymethod /x/y/%s", "name"))
	b.Get("/x/y/:name/blah", testHandler("get /x/y/%s/blah", "name"))

	testCases := []reqTest{
		{"GET", "/", "404"},
		{"PUT", "/x", "405 GET"},
		{"GET", "/x/y/z", "get /x/y/z"},
		{"DELETE", "/x/y/z", "delete /x/y/z"},
		{"MYMETHOD", "/x/y/z", "mymethod /x/y/z"},
		{"POST", "/x/y/z", "405 DELETE, GET, MYMETHOD, PUT"},
		{"GET", "/x/y/z/blah", "get /x/y/z/blah"},
		{"PUT", "/x/y/z/blah", "405 GET"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestNonStandardMethod(t *testing.T) {
	b := NewBuilder()
	b.Get("/x/y", testHandler("a"))
	b.Handle("MYMETHOD", "/x/y", testHandler("b"))

	testCases := []reqTest{
		{"GET", "/x/y", "a"},
		{"MYMETHOD", "/x/y", "b"},
		{"MYMETHOD", "/x", "404"},
		{"PUT", "/x/y", "405 GET, MYMETHOD"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestNestedMuxes(t *testing.T) {
	b0 := NewBuilder()
	b0.Get("/x", testHandler("a"))
	b0.Get("/y", testHandler("b")) // shadowed
	b0.Get("/a/:p", testHandler("c %s", "p"))
	b0.Get("/b/:q", testHandler("d p=%s q=%s", "p", "q"))
	b0.Get("/x%2fy/:foo", testHandler("escape %s", "foo"))
	b0.Get("/c/:foo", testHandler("params %s %s", "p", "foo"))
	b0.Get("/d/*", testHandler("* %s", "*"))
	mux0 := b0.Build()

	b1 := NewBuilder()
	b1.Get("/x/y", testHandler("f"))
	b1.Get("/x/:p:int32", testHandler("g %s", "p"))
	b1.Prefix("/x/", mux0)
	b1.Prefix("/:p/", mux0)

	testCases := []reqTest{
		{"GET", "/x/y", "f"},
		{"GET", "/x/123", "g 123"},
		{"GET", "/x/x", "a"},
		{"GET", "/y/a/z", "c z"},
		{"GET", "/y/b/z", "d p=y q=z"},
		{"GET", "/x/x%2fy/%61%2f%62", "escape a/b"},
		{"GET", "/%62%2fcd/c/e%66g%2f%68", "params b/cd efg/h"},
		{"GET", "/y/d/alpha/bravo", "* /alpha/bravo"},
	}
	testRequests(t, b1.Build(), testCases)
}

func TestSlashMatching(t *testing.T) {
	b := NewBuilder()
	b.Get("/", testHandler("index"))
	b.Get("/hello/", testHandler("hello"))
	b.Get("/world", testHandler("world"))
	b.Get("/wild/*", testHandler("wild"))

	testCases := []reqTest{
		{"GET", "/", "index"},
		{"GET", "/hello", "404"},
		{"GET", "/hello/", "hello"},
		{"GET", "/world", "world"},
		{"GET", "/world/", "404"},
		{"GET", "/wild", "404"},
		{"GET", "/wild/", "wild"},
		{"GET", "/wild/a/b/c", "wild"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestWildcard(t *testing.T) {
	b := NewBuilder()
	b.Get("/", testHandler("index"))
	b.Get("/*", testHandler("wild %s", "*"))

	testCases := []reqTest{
		{"GET", "/", "index"},
		{"GET", "/a/b", "wild /a/b"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestPrefix(t *testing.T) {
	b := NewBuilder()
	b.Get("/", testHandler("index"))
	b.Prefix("/", testHandler("/ prefix %s", "*"))
	b.Prefix("/sub0", testHandler("/sub0 prefix %s", "*"))
	b.Prefix("/sub1/", testHandler("/sub1 prefix %s", "*"))
	b.Prefix("/sub2/*", testHandler("/sub2 prefix %s", "*"))

	testCases := []reqTest{
		{"GET", "/", "index"},
		{"GET", "/a/b", "/ prefix /a/b"},
		{"POST", "/", "/ prefix /"},
		{"POST", "/a/b", "/ prefix /a/b"},
		{"GET", "/sub0", "/ prefix /sub0"},
		{"GET", "/sub1", "/ prefix /sub1"},
		{"GET", "/sub2", "/ prefix /sub2"},
		{"GET", "/sub0xyz", "/ prefix /sub0xyz"},
		{"GET", "/sub1xyz", "/ prefix /sub1xyz"},
		{"GET", "/sub2xyz", "/ prefix /sub2xyz"},
		{"GET", "/sub0/", "/sub0 prefix /"},
		{"GET", "/sub1/", "/sub1 prefix /"},
		{"GET", "/sub2/", "/sub2 prefix /"},
		{"GET", "/sub0/a/b", "/sub0 prefix /a/b"},
		{"GET", "/sub1/a/b", "/sub1 prefix /a/b"},
		{"GET", "/sub2/a/b", "/sub2 prefix /a/b"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestPathEncoding(t *testing.T) {
	b := NewBuilder()
	b.Get("/abc/:foo/def", testHandler("%s", "foo"))
	b.Get("/%3aparam%3aint32/foo", testHandler("fake param"))
	b.Get("/xyz/*", testHandler("xyz %s", "*"))
	b.Get("/%61%2f%62c/:foo/def", testHandler("escape %s", "foo"))
	b.Get("/./a%2f/..", testHandler("non-canonical"))

	testCases := []reqTest{
		{"GET", "/abc/xyz/def", "xyz"},
		{"GET", "/abc/x%79%7a/def", "xyz"},
		{"GET", "/abc/x%2f%79/def", "x/y"},
		{"GET", "/abc/:xyz/def", ":xyz"},
		{"GET", "/xyz/a/b", "xyz /a/b"},
		{"GET", "/xyz/a%2f%62", "xyz /a/b"},
		{"GET", "/a%2f%62%63/x%2fy/d%65f", "escape x/y"},
		{"GET", "/a/bc/x/def", "404"},
		{"GET", "/%2E/a%2f/%2E%2E", "non-canonical"},
		{"GET", "/:param:int32/foo", "fake param"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestParams(t *testing.T) {
	b := NewBuilder()
	b.Get("/:string:string", testHandler("string %s", "string"))
	b.Get(
		"/:int32:int32",
		testHandler(
			"int32 string=%s int=%d int32=%d int64=%d",
			"int32",
			"int32:int",
			"int32:int32",
			"int32:int64",
		),
	)
	b.Get("/:int64:int64", testHandler("int64 %d", "int64:int64"))
	b.Get(
		"/x/:int64:int64",
		testHandler(
			"/x/int64 int=%d int64=%d",
			"int64:int",
			"int64:int64",
		),
	)
	b.Get("/y/:foo/", testHandler("trailing slash %s", "foo"))
	b.Get("/yy/:foo", testHandler("no trailing slash %s", "foo"))
	b.Get("/z/:f%6fo", testHandler("foo %s", "f%6fo")) // param name isn't escaped
	b.Get(
		"/multi/:alpha/0/:beta/1/:gamma",
		testHandler(
			"multi alpha=%s beta=%s gamma=%s",
			"alpha",
			"beta",
			"gamma",
		),
	)

	testCases := []reqTest{
		{"GET", "/", "404"},
		{"GET", "/a/b/c", "404"},
		{"GET", "/abc", "string abc"},
		{"GET", "/abc123", "string abc123"},
		{"GET", "/123abc", "string 123abc"},
		{"GET", "/123", "int32 string=123 int=123 int32=123 int64=123"},
		{"GET", "/0", "int32 string=0 int=0 int32=0 int64=0"},
		{"GET", "/-1", "int32 string=-1 int=-1 int32=-1 int64=-1"},
		{"GET", "/-2147483648", "int32 string=-2147483648 int=-2147483648 int32=-2147483648 int64=-2147483648"},
		{"GET", "/-2147483649", "int64 -2147483649"},
		{"GET", "/-9223372036854775808", "int64 -9223372036854775808"},
		{"GET", "/-9223372036854775809", "string -9223372036854775809"},
		{"GET", "/2147483647", "int32 string=2147483647 int=2147483647 int32=2147483647 int64=2147483647"},
		{"GET", "/2147483648", "int64 2147483648"},
		{"GET", "/9223372036854775807", "int64 9223372036854775807"},
		{"GET", "/9223372036854775808", "string 9223372036854775808"},
		{"GET", "/x/-123", "/x/int64 int=-123 int64=-123"},
		{"GET", "/x/123", "/x/int64 int=123 int64=123"},
		{"GET", "/y/123", "404"},
		{"GET", "/y/123/", "trailing slash 123"},
		{"GET", "/yy", "string yy"},
		{"GET", "/yy/", "404"},
		{"GET", "/yy/123", "no trailing slash 123"},
		{"GET", "/multi/a/0/b/1/c", "multi alpha=a beta=b gamma=c"},
		{"GET", "/multi//0/b/1/c", "308 /multi/0/b/1/c"},
		{"GET", "/z/abc", "foo abc"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestMalformedPattern(t *testing.T) {
	for _, tt := range []struct {
		pat  string
		want interface{}
	}{
		{"/a//", errPatternSlash},
		{"a/", errPatternWithoutSlash},
		{"/a*/b", errSegmentStar},
		{"/a*b", errSegmentStar},
		{"/a/b*/", errSegmentStar},
		{"/:", errEmptyParamName},
		{"/:/foo", errEmptyParamName},
		{"/::int32", errEmptyParamName},
		{"/::", errEmptyParamName},
		{"/::/x", errEmptyParamName},
		{"/:x:x", "unknown parameter type"},
		{"/:x:str", "unknown parameter type"},
		{"/:x:int", "unknown parameter type"},
		{"/:x:", "unknown parameter type"},
		{"/:x/:y/:x:int32", "duplicate parameter"},
	} {
		mux := NewBuilder()
		err := mux.handle("GET", tt.pat, testHandler("x"))
		if err == nil {
			t.Errorf(`handle("GET", %q, h): got nil; want %q`, tt.pat, tt.want)
			continue
		}
		if s, ok := tt.want.(string); ok {
			if !strings.Contains(err.Error(), s) {
				t.Errorf(`handle("GET", %q, h): got %q; want substring %q`, tt.pat, err, s)
			}
			continue
		}
		if err != tt.want {
			t.Errorf(`handle("GET", %q, h): got %q; want %q`, tt.pat, err, tt.want)
		}
	}
}

func TestPrefixErrors(t *testing.T) {
	for _, tt := range []struct {
		pat  string
		want string
	}{
		{"bad", "pattern does not begin"},
		{"", "empty"},
		{"*", "*"},
	} {
		var got string
		func() {
			defer func() {
				if x := recover(); x != nil {
					got, _ = x.(string)
				}
			}()
			NewBuilder().Prefix(tt.pat, testHandler(""))
		}()
		if got == "" {
			t.Errorf("Prefix(%q, *): got nil error; want %q", tt.pat, tt.want)
			continue
		}
		if !strings.Contains(got, tt.want) {
			t.Errorf("Prefix(%q, *):\ngot %q\nwant substring %q", tt.pat, got, tt.want)
		}
	}
}

func TestHandleConflict(t *testing.T) {
	type testRule struct {
		method string
		pat    string
	}
outer:
	for _, rules := range [][]testRule{ // last pattern of sequence should conflict
		{
			{method: "GET", pat: "/x"},
			{method: "GET", pat: "/x"},
		},
		{
			{method: "", pat: "/x"},
			{method: "", pat: "/x"},
		},
	} {
		b := NewBuilder()
		h := testHandler("x")
		for _, rule := range rules[:len(rules)-1] {
			err := b.handle(rule.method, rule.pat, h)
			if err != nil {
				t.Errorf(`handle(%q, %q, h) (not last): got %s", err)`, rule.method, rule.pat, err)
				continue outer
			}
		}
		rule := rules[len(rules)-1]
		err := b.handle(rule.method, rule.pat, h)
		if err == nil {
			t.Errorf(`handle(%q, %q, h) (last): got nil error; want conflict`, rule.method, rule.pat)
			continue
		}
		if !strings.Contains(err.Error(), "conflicts with previously registered pattern") {
			t.Errorf(`handle(%q, %q, h) (last): got %s; want conflict error`, rule.method, rule.pat, err)
			continue
		}
	}
}

func TestServeFile(t *testing.T) {
	td := t.TempDir()
	for _, f := range []struct {
		name string
		data string
		perm fs.FileMode
	}{
		{"x.txt", "hello world", 0o644},
		{"perm.txt", "", 0o000},
	} {
		name := filepath.Join(td, f.name)
		if err := os.WriteFile(name, []byte(f.data), f.perm); err != nil {
			t.Fatalf("error writing %s: %s", name, err)
		}
	}

	b := NewBuilder()
	b.ServeFile("/x", filepath.Join(td, "x.txt"))
	b.ServeFile("/perm", filepath.Join(td, "perm.txt"))

	testCases := []reqTest{
		{"GET", "/x", "hello world"},
		{"HEAD", "/x", ""},
		{"POST", "/x", "405 GET, HEAD"},
		{"GET", "/perm", "403"},
	}
	testRequests(t, b.Build(), testCases)
}

func TestServeFS(t *testing.T) {
	fsys := fstest.MapFS{
		"hello.txt": &fstest.MapFile{
			Data: []byte("hello world"),
		},
		"z/hello.txt": &fstest.MapFile{
			Data: []byte("hello z"),
		},
		"perm.txt": &fstest.MapFile{
			Data: []byte(""),
		},
	}
	b := NewBuilder()
	b.ServeFS("/x/y", fsys)

	testCases := []reqTest{
		{"GET", "/x/y/hello.txt", "hello world"},
		{"HEAD", "/x/y/hello.txt", ""},
		{"GET", "/x/y/z/hello.txt", "hello z"},
	}
	testRequests(t, b.Build(), testCases)
}

type reqTest struct {
	method string
	path   string
	want   string
}

func testRequests(t *testing.T, mux *Mux, tests []reqTest) {
	t.Helper()
	for _, tt := range tests {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tt.method, tt.path, nil)
		mux.ServeHTTP(w, r)

		switch {
		case tt.want == "404" || tt.want == "403":
			want, err := strconv.Atoi(tt.want)
			if err != nil {
				panic("can't happen")
			}
			if w.Code == 200 {
				t.Errorf("%s %s: got status 200 [%s] instead of %d",
					tt.method, tt.path, w.Body, want)
			} else if w.Code != want {
				t.Errorf("%s %s: got status %d instead of %d",
					tt.method, tt.path, w.Code, want)
			}
		case strings.HasPrefix(tt.want, "405 "):
			if w.Code != 405 {
				t.Errorf("%s %s: got status %d instead of 405",
					tt.method, tt.path, w.Code)
				continue
			}
			allow := strings.TrimPrefix(tt.want, "405 ")
			if got := w.Result().Header.Get("Allow"); got != allow {
				t.Errorf("%s %s: got 405 response with Allow=%q instead of %q",
					tt.method, tt.path, got, allow)
			}
		case strings.HasPrefix(tt.want, "308 "):
			if w.Code != 308 {
				t.Errorf("%s %s: got status %d instead of 308",
					tt.method, tt.path, w.Code)
				continue
			}
			targ := strings.TrimPrefix(tt.want, "308 ")
			if got := w.Result().Header.Get("Location"); got != targ {
				t.Errorf("%s %s: got 308 redirect to %q instead of %q",
					tt.method, tt.path, got, targ)
			}
		case w.Code != 200:
			t.Errorf("%s %s: got status %d instead of 200",
				tt.method, tt.path, w.Code)
		default:
			got := w.Body.String()
			if got != tt.want {
				t.Errorf("%s %s: got %q; want %q", tt.method, tt.path, got, tt.want)
			}
		}
	}
}

func testHandler(format string, params ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := RequestParams(r)
		args := make([]interface{}, len(params))
		for i, pn := range params {
			if pn, ok := trimSuffix(pn, ":int32"); ok {
				args[i] = p.Int32(pn)
			} else if pn, ok := trimSuffix(pn, ":int64"); ok {
				args[i] = p.Int64(pn)
			} else if pn, ok := trimSuffix(pn, ":int"); ok {
				args[i] = p.Int(pn)
			} else if pn == "*" {
				args[i] = p.Wildcard()
			} else {
				args[i] = p.Get(pn)
			}
		}
		fmt.Fprintf(w, format, args...)
	}
}
