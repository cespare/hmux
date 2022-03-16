# hmux

**THIS IS A WORK IN PROGRESS. IT IS NOT YET READY FOR USE.**

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

* Pattern special case: `""` to match any URI. (Useful for CONNECT requests, for
  example, which give an authority as the URI.)
* Special redirect/cleaning behavior for CONNECT requests?
* Flesh out package doc
  - examples
    * Nested muxes
    * File serving
    * CORS OPTIONS
    * Hooking up a reverse proxy
  - Exact precedence rules
  - Valid patterns (and panics)
  - All patterns start with /
  - Redirects
* Flesh out README with links and short example
* Add some benchmarks
  - Routing
  - Param extraction
  - Reduce allocs

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
