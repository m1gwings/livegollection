package pool

// Code inspired from https://github.com/gorilla/websocket/blob/master/examples/chat/client.go .
import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	// Used to set WriteDeadline.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	// Used to set ReadDeadline.
	pongWait = 60 * time.Second

	// Send pings to peer with this period.
	// It must be less then pongWait, to prevent going past the ReadDeadline.
	pingPeriod = (pongWait * 9) / 10
)

// message represents the generic message exchanged over websocket connection.
// It holds a messageType {binary, text, ...} and the actual data in bytes.
type message struct {
	messageType int
	data        []byte
}

// client handles reading and writing over the connection with a specific peer.
type client struct {
	// conn is the websocket connection object.
	conn *websocket.Conn

	// writeQueue collects all the messages that have to be delivered to the peer.
	// These messages will be handled by handleWriteQueue().
	writeQueue chan *message

	// toClose is used to prevent a goroutine to close the connection,
	// because an error has occured, while the other is still active.
	// Goroutines in question are handleReadQueue and handleWriteQueue.
	// When handleWriteQueue (or handleReadQueue) encounters an error,
	// it sends true on toClose channel and exits.
	// Then handleReadQueue (or handleWriteQueue) will call client.close(),
	// since now there is no other goroutine active, and exit.
	toClose chan bool

	// closed is true if conn.Close() has been called.
	// If closed is true, it's possible to delete the client object without worrying
	// of goroutine leaks.
	closed bool

	// pool is a reference to the Pool to whom this client belongs.
	pool *Pool
}

// newClient returns a *client after having initialized it properly.
// It also starts the concurrent goroutines to handle read and write queues.
func newClient(conn *websocket.Conn, pool *Pool) *client {
	c := &client{conn: conn,
		writeQueue: make(chan *message, 1), toClose: make(chan bool, 1),
		pool: pool}

	go c.handleWriteQueue()
	go c.handleReadQueue()

	return c
}

// TODO: solve the problem in a more elegant way.
// clientError adds the prefix "client: " to a generic error.
func clientError(err error) error {
	return fmt.Errorf("client: %v", err)
}

// close calls conn.Close() (and handles the eventual error).
// It also sets closed = true.
func (c *client) close() {
	if err := c.conn.Close(); err != nil {
		c.pool.logError(clientError(err))
	}
	c.closed = true
}

func (c *client) handleWriteQueue() {
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()

	for {
		var messageType int
		var data []byte

		select {
		case <-c.toClose:
			c.close()
			return
		case <-pingTicker.C:
			messageType, data = websocket.PingMessage, nil
		case mess := <-c.writeQueue:
			messageType, data = mess.messageType, mess.data
		}

		c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(messageType, data); err != nil {
			c.pool.logError(clientError(err))
			c.toClose <- true
			return
		}
	}
}

func (c *client) handleReadQueue() {
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		select {
		case <-c.toClose:
			c.close()
			return
		default:
			messageType, data, err := c.conn.ReadMessage()
			if err != nil {
				c.pool.logError(clientError(err))
				c.toClose <- true
				return
			}

			if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
				c.pool.logError(clientError(fmt.Errorf("unexpected messageType: %d", messageType)))
				continue
			}

			c.pool.readQueue <- &message{messageType, data}
		}
	}
}
