package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cespare/hmux"
)

func main() {
	m := hmux.New()

	m.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("index\n"))
	})
	m.Get("/x/y", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from /x/y")
	})
	m.Get("/:foo:int64/z", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r.Context())
		fmt.Fprintf(w, "Hello: %d\n", p.Int64("foo"))
	})
	m.Get("/x/:foo", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r.Context())
		fmt.Fprintf(w, "Hello: %s\n", p.Get("foo"))
	})
	m.Get("/x/*", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r.Context())
		fmt.Fprintf(w, "Hello wildcard: %s\n", p.Wildcard())
	})

	log.Fatal(http.ListenAndServe("localhost:8888", m))
}
