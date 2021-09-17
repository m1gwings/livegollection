package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/m1gwings/livegollection/pool"
)

func main() {
	p := pool.NewPool(log.Default())

	go func() {
		for {
			data := p.ReadNextMessageInQueue()
			log.Printf("%s\n", string(data))
			p.SendMessageToAll(websocket.TextMessage, data)
		}
	}()

	http.HandleFunc("/ws", p.ConnHandlerFunc)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/script", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "script.js")
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
