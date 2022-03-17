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
