package hmux_test

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/cespare/hmux"
)

func Example_basics() {
	b := hmux.NewBuilder()
	b.Get("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, world!")
	})
	b.Get("/hello/:name", func(w http.ResponseWriter, r *http.Request) {
		name := hmux.RequestParams(r).Get("name")
		fmt.Fprintf(w, "Hello, %s!\n", name)
	})
	mux := b.Build()
	log.Fatal(http.ListenAndServe(":5555", mux))
}

func staticHandler(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, msg)
	}
}

func checkUser(h http.Handler) http.Handler {
	hf := func(w http.ResponseWriter, r *http.Request) {
		// Check that the user is logged in or redirect to login page...
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(hf)
}

func checkAdmin(h http.Handler) http.Handler {
	hf := func(w http.ResponseWriter, r *http.Request) {
		// Check that the logged-in user has admin permissions
		// and respond with http.StatusForbidden otherwise...
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(hf)
}

func Example_nestedMuxes() {
	adminBuilder := hmux.NewBuilder()
	adminBuilder.Get("/", staticHandler("Hello from the admin page"))
	adminBuilder.Get("/users", staticHandler("List of all users"))
	admin := checkAdmin(adminBuilder.Build())

	b := hmux.NewBuilder()
	b.Get("/", staticHandler("Main page"))
	b.Get("/profile", staticHandler("User profile"))
	b.Prefix("/admin/", admin)
	mux := checkUser(b.Build())

	log.Fatal(http.ListenAndServe(":5555", mux))
}

func Example_fileServing() {
	b := hmux.NewBuilder()
	b.Get("/", staticHandler("Main page"))
	b.ServeFS("/static/", os.DirFS("static"))
	mux := b.Build()
	log.Fatal(http.ListenAndServe(":5555", mux))
}

func Example_catchAll() {
	b := hmux.NewBuilder()
	b.Get("/x", staticHandler("x"))
	// The empty pattern matches all paths and an empty method matches all
	// methods, so it's possible to construct a rule that catches all
	// requests as a fallback. This means that hmux's built-in 404 and 405
	// handling will never be used.
	b.Handle("", "", staticHandler("caught!"))
	mux := b.Build()
	log.Fatal(http.ListenAndServe(":5555", mux))
}
