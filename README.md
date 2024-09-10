# hmux

**THIS PACKAGE IS DEPRECATED.**

In Go 1.22, the standard library's [`http.ServeMux`](https://tip.golang.org/doc/go1.22#enhanced_routing_patterns)
was overhauled and now provides the essential features that hmux provided,
including method-based routing and path params.

See also [the Go 1.22 release notes](https://tip.golang.org/doc/go1.22#enhanced_routing_patterns)
for details.

Therefore, this package is no longer needed and has been deprecated. You should
use `http.ServeMux` instead.

---

[![Go Reference](https://pkg.go.dev/badge/github.com/cespare/hmux.svg)](https://pkg.go.dev/github.com/cespare/hmux)

This package provides an HTTP request multiplexer which matches requests to
handlers using method- and path-based rules.

Here's a simple example:

``` go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cespare/hmux"
)

func main() {
	b := hmux.NewBuilder()
	b.Get("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "index")
	})
	b.Get("/hello/:name", func(w http.ResponseWriter, r *http.Request) {
		name := hmux.RequestParams(r).Get("name")
		fmt.Fprintf(w, "Hello, %s!\n", name)
	})
	mux := b.Build()
	log.Fatal(http.ListenAndServe("localhost:8888", mux))
}
```

If this server is running, then:

```
$ curl localhost:8888/hello/alice
Hello, alice!
```

Consult the documentation for the details about this package and more examples.
