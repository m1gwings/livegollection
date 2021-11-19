package main

import (
	"context"
	"log"
	"net/http"

	"github.com/m1gwings/livegollection"
)

func main() {
	coll := livegollection.NewDummyCollection()

	coll.Create(livegollection.DummyItem{String: "abcd", Num: 1})

	lC := livegollection.NewLiveCollection(context.TODO(), coll, log.Default())

	http.HandleFunc("/ws", lC.Join)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/script", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "script.js")
	})
	http.ListenAndServe("localhost:8080", nil)
}
