# hmux

TODO

## Decisions

* Shorthand helpers for these methods: GET, POST, PUT, DELETE, HEAD
  - These seem to be the common set for REST-y stuff
  - PATCH, OPTIONS, CONNECT, TRACE are provided by other frameworks but don't
    seem common enough to warrant dedicated functions
* No chaining -- this isn't idiomatic for Go.
* No regular expressions.
* Each path segment has a single parameter. This sidesteps a whole lot of
  complicated ambiguity questions.
* Precedence rules:
	1. Segment-by-segment comparison, with this precedence:
	   1. literal
	   2. int32 param
	   3. int64 param
	   4. string param
	   5. wildcard
	2. Depth of match (more segments wins)
	3. Specific method vs. catch-all
* Instead of the two-phases-of-operation thing (where you need to add rules
  before using the mux), which raises questions about what the acceptable use
  is, use a self-explanatory API with separate Builder and Mux types. This also
  creates a natural point at which to build a trie.

## TODO:

* File serving? (Or not needed?)
* Flesh out package doc
  - examples
    * Nested muxes
    * File serving
    * CORS OPTIONS
  - Exact precedence rules
  - Valid patterns (and panics)
  - All patterns start with /
  - Redirects
* Flesh out README with links and short example

## Open questions

### Missing params

Right now, `params.Get("doesNotExist")` panics. This is to catch typos, since
param names are just strings. As long as you exercise the route in dev, the
panic would let you catch the mistake and also makes it hard to ignore.

In converting a bunch of code to use hmux, I found a few projects which were
doing the equivalent of this:

```
Get("/xyz", handleXYZ)
Get("/xyz/:k", handleXYZ)

...

func handleXYZ(w http.ResponseWriter, r *http.Request) {
	k := hmux.RequestParams(r.Context()).Get("k") // assume it returns "" instead of panicking

	// do something slightly different depending on whether k == ""
}
```

Should we change to use this behavior?

I'm leaning toward no: the panicking seems helpful in most use cases and it's
possible to implement the desired configuration, though not as concisely, using
a bit of per-handler logic:

```
Get("/xyz", func(w http.ResponseWriter, r *http.Request) {
	handleXYZ(w, r, "")
})
Get("/xyz/:k", func(w http.ResponseWriter, r *http.Request) {
	k := hmux.RequestParams(r.Context()).Get("k")
	handleXYZ(w, r, k)
})

...

func handleXYZ(w http.ResponseWriter, r *http.Request, k string) { ... }
```

### File serving

When converting code to use hmux, the typical pattern for file serving is:

    builder.Prefix("/static", http.FileServer(http.Dir(staticDir)))

This is a tiny bit verbose. We could shorten it up with a dedicated helper

    builder.ServeFiles("/static", http.FS(staticPath))

That seems like not enough benefit to warrant the extra method, though.

Even with that helper, we're still using the http.FileSystem interface which is
somewhat vestigial after fs.FS. We could fix that at the same time by writing
our own file server:

    builder.ServeFiles("/static", os.DirFS(staticPath))

This custom server could also not list directories, which is a feature of
http.FileServer that I almost never want.

### Serving an individual file

Serving a single file is verbose:

    builder.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
            http.ServeFile(w, r, "static/favicon.ico")
    })

Should we have a helper just for that?

    builder.ServeFile("/favicon.ico", "static/favicon.ico")

I'm leaning toward no. Projects with lots of these can make a small helper. For
most of the projects I've converted, it was used only once, for favicon.ico.

### Param extraction

Extracting a single param is a little verbose:

    name := hmux.RequestParams(r.Context()).Get("name")

One change we could consider is adding a helper for fetching a single param:

    name := hmux.Get(r, "name")

That doesn't seem like a very good idea, though; the savings are small at it
encourages people not to reuse the params pulled out of the context (which
implies allocation?).

A smaller change we could make would be for `RequestParams` to take an
`*http.Request` rather than a `context.Context`. `RequestParams` shouldn't need
access to the whole request state, but on the other hand virtually every caller
will do `hmux.RequestParams(r.Context())` so maybe it's an improvement just to
let them write `hmux.RequestParams(r)`.
