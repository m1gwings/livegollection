package pool

// Code inspired from https://github.com/gorilla/websocket/blob/master/examples/chat/client.go.
import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

type message struct {
	messageType int
	data        []byte
}

type client struct {
	conn       *websocket.Conn
	writeQueue chan *message
	toClose    chan bool
	closed     bool
	pool       *Pool
}

func newClient(conn *websocket.Conn, pool *Pool) *client {
	c := &client{conn: conn,
		writeQueue: make(chan *message, 1), toClose: make(chan bool, 1),
		pool: pool}

	go c.handleWriteQueue()
	go c.handleReadQueue()

	return c
}

func clientError(err error) error {
	return fmt.Errorf("client: %v", err)
}

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
