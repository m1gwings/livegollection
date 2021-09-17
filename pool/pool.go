package pool

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Pool struct {
	clients       map[int]*client
	autoIncrement int
	mu            sync.Mutex

	readQueue chan *message

	logger *log.Logger
}

func NewPool(l *log.Logger) *Pool {
	return &Pool{clients: make(map[int]*client),
		readQueue: make(chan *message), logger: l}
}

func (p *Pool) logError(err error) {
	if p.logger == nil {
		return
	}

	p.logger.Printf("livegollection: %v\n", err)
}

func poolError(err error) error {
	return fmt.Errorf("pool: %v", err)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (p *Pool) ConnHandlerFunc(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logError(poolError(err))
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.clients[p.autoIncrement] = newClient(conn, p)
	p.autoIncrement++
}

func (p *Pool) sendMessage(key int, mess *message) {
	c, ok := p.clients[key]
	if !ok {
		p.logError(poolError(fmt.Errorf("unexpected clients key: %d", key)))
		return
	}

	if c.closed {
		delete(p.clients, key)
		return
	}

	c.writeQueue <- mess
}

func (p *Pool) SendMessageToAll(messageType int, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var wg sync.WaitGroup

	for key := range p.clients {
		wg.Add(1)

		go func(key int) {
			p.sendMessage(key, &message{messageType, data})
			wg.Done()
		}(key)
	}

	wg.Wait()
}

func (p *Pool) ReadNextMessageInQueue() []byte {
	mess := <-p.readQueue
	return mess.data
}
