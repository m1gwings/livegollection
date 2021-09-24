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
	// map of clients in the pool.
	clients map[int]*client

	// autoIncrement ensures that each client is represented by a unique integer key.
	autoIncrement int

	// mu is used to prevent race conditions on write operations to clients or autoIncrement.
	mu sync.Mutex

	// readQueue is the channel where all the messages from
	// clients in the pool are serialized.
	readQueue chan *message

	// logger, if set, is used to log all the errors encountered.
	logger *log.Logger
}

// NewPool creates and properly initialize a pool.
func NewPool(l *log.Logger) *Pool {
	return &Pool{clients: make(map[int]*client),
		readQueue: make(chan *message), logger: l}
}

// logError, after having checked that the logger has been set,
// logs errors, adding to them the special prefix: "livegollection ".
func (p *Pool) logError(err error) {
	if p.logger == nil {
		return
	}

	p.logger.Printf("livegollection: %v\n", err)
}

// TODO: solve the problem in a more elegant way.
// poolError adds the prefix "pool: " to a generic error.
func poolError(err error) error {
	return fmt.Errorf("pool: %v", err)
}

// upgrader is used to upgrade the websocket connection of new clients.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// ConnHandlerFunc can be used as an http.HandlerFunc for an endpoint.
// Every GET request to the endpoint, if properly formatted,
// will be upgraded to a websocket connection and added to the pool.
// This is the intended way to add clients to the pool.
func (p *Pool) ConnHandlerFunc(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logError(poolError(err))
		return
	}

	// We need to lock the mutex since we are executing write operations on clients map.
	// Furhtermore for p.autoIncrement++ which is also susceptible for race conditions.
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clients[p.autoIncrement] = newClient(conn, p)
	p.autoIncrement++
}

// sendMessage is an helper function used to send a message to a specific client.
// key is the integer key that identifies the client in clients map.
// It is important that there is atmost one goroutine at the time running
// sendMessage with the same value for key in order to prevent race conditions
// when deleting the client from the map.
func (p *Pool) sendMessage(key int, mess *message) {
	c, ok := p.clients[key]

	// If the program is working as intended, this condition will never be true.
	if !ok {
		p.logError(poolError(fmt.Errorf("unexpected clients key: %d", key)))
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
func (p *Pool) SendMessageToAll(messageType int, data []byte) {
	// We need to use a mutex to prevent race conditions.
	// Indeed inside this function we call sendMessage,
	// which could execute delete on clients map (if c.closed is true).
	// Furthermore this way we preserve the order of delivery of messages.
	// TODO: Try locking the mutex directly inside sendMessage, order of delivery of messages should be preserved anyway.
	p.mu.Lock()
	defer p.mu.Unlock()

	// To make the mutex lock effective we need to keep the function
	// running until all the concurrent sendMessage have finished.
	// For this reason we use a wait group.
	var wg sync.WaitGroup

	for key := range p.clients {
		wg.Add(1)

		// It is important to pass key as a value to the goroutine.
		// The outer loop key changes during iterations.
		go func(key int) {
			p.sendMessage(key, &message{messageType, data})
			wg.Done()
		}(key)
	}

	wg.Wait()
}

// ReadNextMessageInQueue is a blocking operation that eventually will return
// the next message in read queue, where all the messages from clients are serialized.
// Sender of the message could be any of the clients in the pool.
func (p *Pool) ReadNextMessageInQueue() []byte {
	mess := <-p.readQueue
	return mess.data
}
