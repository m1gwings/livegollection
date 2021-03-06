package pool

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Pool manages a group of websocket connections.
// It is responsible for upgrading connections of new clients,
// reading and serializing messages from them.
// It also allows to broadcast a message to the entire pool.
type Pool struct {
	// clients is a map of clients in the pool.
	clients map[int]*client

	// autoIncrement ensures that each client is represented by a unique integer key.
	autoIncrement int

	// mu is used to prevent race conditions on write operations to clients or autoIncrement.
	mu sync.Mutex

	// readQueue is the channel where all the bodies of messages from
	// clients to the pool are serialized.
	readQueue chan []byte

	// logger, if set, is used to log all the errors encountered.
	logger *log.Logger
}

// NewPool creates and properly initializes a pool.
func NewPool(l *log.Logger) *Pool {
	return &Pool{
		clients:   make(map[int]*client),
		readQueue: make(chan []byte),
		logger:    l,
	}
}

// logError, after having checked that the logger has been set,
// logs errors, adding to them the special prefix: "livegollection: ".
func (p *Pool) logError(err error) {
	if p.logger == nil {
		return
	}

	p.logger.Printf("livegollection: %v\n", err)
}

// upgrader is used to upgrade the websocket connection of new clients.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// AddToPool is the intended way to add a client to the pool.
// It can be used inside an http.HandlerFunc.
// initialMessages is the array of message bodies that are going to be sent to the
// client before every other update from the pool.
// messageType specifies the type of each one of the initial messages.
func (p *Pool) AddToPool(w http.ResponseWriter, r *http.Request,
	messageType int, initialMessages [][]byte) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logError(fmt.Errorf("error in AddToPool after upgrading the connection: %v", err))
		return
	}

	// We need to create the client before locking the mutex,
	// since we don't want to lock the entire pool while we send initial messages to the new client.
	// It's important to create new clients using newClient factory function
	// in order to inizialize them properly.
	c := newClient(conn, p)
	for _, data := range initialMessages {
		c.writeQueue <- &message{messageType: messageType, data: data}
	}

	// We need to lock the mutex since we are executing write operations on clients map.
	// Furhtermore for p.autoIncrement++ which is also susceptible for race conditions.
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clients[p.autoIncrement] = c
	p.autoIncrement++
}

// sendMessage is an helper function used to send a message to a specific client.
// key is the integer key that identifies the client in clients map.
// Do NOT run multiple instances of this function in separate goroutines (even if the value for key is different)
// because it could lead to race condition errors in map access.
func (p *Pool) sendMessage(key int, mess *message) {
	c, ok := p.clients[key]

	// If the program is working as intended, this condition will never be true.
	if !ok {
		p.logError(fmt.Errorf("error in sendMessage: unexpected client key: %d", key))
		return
	}

	// Lazy deletion of closed clients.
	if c.closed {
		// If c.closed is true we are sure that all the goroutines associated to
		// the client have exited (or will exit soon), so we can delete the client
		// from the map without worrying about goroutine leaks.
		delete(p.clients, key)
		return
	}

	// Client's handleWriteQueue will take care of the message.
	c.writeQueue <- mess
}

// SendMessageToAll broadcasts data to all the clients in the pool.
// messageType is the integer associated to the type of websocket message,
// an example value for messageType is websocket.TextMessage where
// websocket refers to github.com/gorilla/websocket package.
func (p *Pool) SendMessageToAll(messageType int, data []byte) {
	// We need to use a mutex because we are reading from clients map.
	// Furthermore inside this function we call sendMessage,
	// which could execute delete on clients map (if c.closed is true).
	// In this way we also preserve the order of delivery of messages.
	p.mu.Lock()
	defer p.mu.Unlock()

	for key := range p.clients {
		p.sendMessage(key, &message{messageType, data})
	}
}

// ReadQueue returns the readQueue of the pool.
// To read a message use <-pool.ReadQueue().
// The choice of returning the channel instead of waiting for the next message and returning it
// is motivated by the advantage in "select" statement of the former approach.
func (p *Pool) ReadQueue() <-chan []byte {
	return p.readQueue
}

// CloseAll shuts down all the clients in the pool.
// Make sure to invoke it before deleting the pool, otherwise it will lead to goroutine leaks.
func (p *Pool) CloseAll() {
	// We need to lock the mutex because we are accessing clients map.
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, c := range p.clients {
		c.cancel()
	}
}
