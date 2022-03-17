# hmux

[![Go Reference](https://pkg.go.dev/badge/github.com/cespare/hmux.svg)](https://pkg.go.dev/github.com/cespare/hmux)

**NOTE: this package is still under active development and has not yet reached
version 1.0.**

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
