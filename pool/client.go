package pool

// Code inspired from https://github.com/gorilla/websocket/blob/master/examples/chat/client.go .
import (
	"context"
	"fmt"
	"sync"
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
	// We are also considering writeWait.
	pingPeriod = pongWait - writeWait
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
	// These messages will be handled by writeQueueHandler().
	writeQueue chan *message

	// pool is a reference to the Pool to whom this client belongs.
	pool *Pool

	// cancel, if invoked, will stop readQueueHandler, writeQueueHandler and subsequently closeHandler.
	// If closed is not true, but you want to shut the client down, make sure to call this function.
	// Otherwise it will lead to goroutine leaks: closeHanlder, readQueueHandler and writeQueueHandler
	// won't stop running.
	cancel context.CancelFunc

	// closed is true if and only if conn.Close() has been called.
	// If closed is true, it's possible to delete the client without worrying about goroutine leaks.
	closed bool
}

// newClient returns a *client after having initialized it properly.
// It is important to create new clients only by this factory function beacuase
// it also starts the concurrent goroutine to handle client shutdown (closeHandler),
// which in turn starts the concurrent goroutines to handle read and write queue.
func newClient(conn *websocket.Conn, pool *Pool) *client {
	ctx, cancel := context.WithCancel(context.Background())

	c := &client{
		conn:       conn,
		writeQueue: make(chan *message, 1),
		pool:       pool,
		cancel:     cancel,
	}

	go c.closeHandler(ctx)

	return c
}

// TODO: solve the problem in a more elegant way.
// clientError adds the prefix "client: " to a generic error.
func clientError(err error) error {
	return fmt.Errorf("client: %v", err)
}

// writeQueueHandler is responsible for sending messages and
// regular pings to the peer.
// It takes the messages to send from the writeQueue channel.
func (c *client) writeQueueHandler(ctx context.Context) {
	// pingTicker is used to send regular pings to the peer.
	// Pings must be send to prevent going past ReadDeadline.
	pingTicker := time.NewTicker(pingPeriod)
	// pingTicker has to be stopped to release associated resources.
	defer pingTicker.Stop()

	for {
		var messageType int
		var data []byte

		select {
		case <-ctx.Done():
			// The cancel function has been invoked, this goroutine should exit.
			return
		case <-pingTicker.C:
			messageType, data = websocket.PingMessage, nil
		case mess := <-c.writeQueue:
			messageType, data = mess.messageType, mess.data
		}

		// Each time we send a message, we set the WriteDeadline before.
		// If the message won't be delivered before the deadline,
		// WriteMessage will return an error and the connection will be corrupted.
		c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(messageType, data); err != nil {
			// If an error occurs we invoke cancel(), so readQueueHandler will be noticed,
			// and exit.
			c.pool.logError(clientError(err))
			c.cancel()
			return
		}
	}
}

// readQueueHandler is responsible for reading and waiting for messages
// and pings from the peer.
// If a message has been read successfully, it will be pushed onto readQueue.
func (c *client) readQueueHandler(ctx context.Context) {
	// Initilization for the read deadline.
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// Every time we receive a pong message, we update the ReadDeadline (keeping the connection alive).
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			// The cancel function has been invoked, this goroutine should exit.
			return
		default:
			messageType, data, err := c.conn.ReadMessage()
			if err != nil {
				// If an error occurs we invoke cancel(), so readQueueHandler will be noticed,
				// and exit.
				c.pool.logError(clientError(err))
				c.cancel()
				return
			}

			// If the program is working as intended, this condition will never be true.
			// We expect messageType to be always text or binary.
			if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
				c.pool.logError(clientError(fmt.Errorf("unexpected messageType: %d", messageType)))
				continue
			}

			// If the message has been read successfully, we can push it onto the readQueue.
			c.pool.readQueue <- &message{messageType, data}
		}
	}
}

// closeHandler is responsible for waiting writeQueueHandler and readQueueHandler to exit
// and then invoke conn.Close(). It will also set closed = true.
func (c *client) closeHandler(ctx context.Context) {
	// wg is necessary to wait for writeQueueHandler and readQueueHandler to exit.
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		c.writeQueueHandler(ctx)
		wg.Done()
	}()
	go func() {
		c.readQueueHandler(ctx)
		wg.Done()
	}()

	wg.Wait()
	if err := c.conn.Close(); err != nil {
		c.pool.logError(clientError(err))
	}

	c.closed = true
}
