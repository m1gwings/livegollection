package pool

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

const htt = "http"
const ws = "ws"

func switchProtocol(url string) string {
	return strings.Replace(url, htt, ws, 1)
}

type poolErrorsWriter struct {
	t *testing.T
}

func (pErr poolErrorsWriter) Write(data []byte) (int, error) {
	pErr.t.Errorf("%s", string(data))
	return len(data), nil
}

func newTestLogger(t *testing.T) *log.Logger {
	return log.New(
		poolErrorsWriter{t: t},
		"error from pool: ",
		log.Lmicroseconds,
	)
}

func newTestPool(t *testing.T) *Pool {
	return NewPool(newTestLogger(t))
}

func newTestServer(pool *Pool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(pool.ConnHandlerFunc))
}

func populate(url string, connsNum int) ([]*websocket.Conn, error) {
	conns := make([]*websocket.Conn, 0, connsNum)

	for i := 0; i < connsNum; i++ {
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			return nil, err
		}
		conns = append(conns, c)
	}

	return conns, nil
}

func TestConnHandlerFunc(t *testing.T) {
	pool := newTestPool(t)
	defer pool.CloseAll()

	testSrv := newTestServer(pool)
	defer testSrv.Close()

	_, err := populate(switchProtocol(testSrv.URL), 10)
	if err != nil {
		t.Error(err)
	}
}

// Taken from gorilla websocket docs and slightly adjusted.
func readLoop(c *websocket.Conn) {
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			c.Close()
			break
		}
	}
}

// IMPORTANT: This test needs at least pongWait seconds to complete for reasons
// explained in a extensive comment in readQueueHandler (inside client.go).
func TestCloseAll(t *testing.T) {
	pool := newTestPool(t)

	testSrv := newTestServer(pool)
	defer testSrv.Close()

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
	}

	pool.CloseAll()

	var closed [connsNum]bool

	var wg sync.WaitGroup

	for i, c := range conns {
		wg.Add(1)

		iCopy := i
		c.SetCloseHandler(func(code int, text string) error {
			closed[iCopy] = true
			wg.Done()
			return nil
		})

		go readLoop(c)
	}

	wg.Wait()

	for i := 0; i < connsNum; i++ {
		if !closed[i] {
			t.Errorf("client %d is not closed", i)
		}
	}
}

func TestSendMessageToAll(t *testing.T) {
	pool := newTestPool(t)
	defer pool.CloseAll()

	testSrv := newTestServer(pool)
	defer testSrv.Close()

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
	}

	const messText = "hello!"

	pool.SendMessageToAll(websocket.TextMessage, []byte(messText))

	for i, c := range conns {
		messageType, p, err := c.ReadMessage()
		if err != nil {
			t.Errorf("error while reading message from client %d: %v", i, err)
		}

		if messageType != websocket.TextMessage {
			t.Errorf("unexpected message type for client %d, got %d, expected %d",
				i, messageType, websocket.TextMessage)
		}

		if string(p) != messText {
			t.Errorf("unexpected message body for client %d, got %s, expected %s",
				i, string(p), messText)
		}
	}
}
