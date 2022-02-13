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
		w.Write([]byte("index\n"))
	})
	b.Get("/x/y", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from /x/y")
	})
	b.Get("/:foo:int64/z", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r)
		fmt.Fprintf(w, "Hello: %d\n", p.Int64("foo"))
	})
	b.Get("/x/:foo", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r)
		fmt.Fprintf(w, "Hello: %s\n", p.Get("foo"))
	})
	b.Get("/x/*", func(w http.ResponseWriter, r *http.Request) {
		p := hmux.RequestParams(r)
		fmt.Fprintf(w, "Hello wildcard: %s\n", p.Wildcard())
	})

	log.Fatal(http.ListenAndServe("localhost:8888", b.Build()))
}
