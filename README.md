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
