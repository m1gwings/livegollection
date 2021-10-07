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

const httpStr = "http"
const wsStr = "ws"

// switchProtocol replaces the http protocol in url with ws. (http://... -> ws://...)
// It is needed because (*httptest.Server).URL returns the endpoint url with http protocol,
// but we need to communicate over websocket protocol.
func switchProtocol(url string) string {
	return strings.Replace(url, httpStr, wsStr, 1)
}

// poolErrorsWriter is used to catch all the errors logged by the pool logger
// and redirect them to t.Error.
type poolErrorsWriter struct {
	t *testing.T
}

// Write method implements the io.Writer interface for poolErrorsWriter.
// In this way we can set poolErrorsWriter as the out stream for the pool logger,
// thus we can catch all the logged errors and report them with t.Error.
func (pErr poolErrorsWriter) Write(data []byte) (int, error) {
	pErr.t.Errorf("%s", string(data))
	return len(data), nil
}

// newTestLogger returns a properly formatted logger with a poolErrorsWriter as the out stream.
// This factory function is used by newTestPool to set the pool logger.
func newTestLogger(t *testing.T) *log.Logger {
	return log.New(
		poolErrorsWriter{t: t},
		"error from pool: ",
		log.Lmicroseconds,
	)
}

// newTestPool returns a properly formatted pool, setting its logger through newTestLogger.
// All the *Pool objects in tests should be created via this factory function,
// in this way all the errors logged by the pool will appear as errors in the output of the test.
func newTestPool(t *testing.T) *Pool {
	return NewPool(newTestLogger(t))
}

// newTestServer returns an http test server after having set pool.ConnHandlerFunc as its handler.
func newTestServer(pool *Pool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(pool.ConnHandlerFunc))
}

// populate opens connsNum connections to the websocket server reachable through url,
// then it returns all the connections in a slice.
func populate(url string, connsNum int) ([]*websocket.Conn, error) {
	// We set the initial length of the slice to 0, and the capacity to connsNum.
	// So we can use append without risk of reallocations.
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

// readLoop is needed in order to process control messages received by c.
func readLoop(c *websocket.Conn) {
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			c.Close()
			break
		}
	}
}

func TestCloseAll(t *testing.T) {
	// This test needs at least pongWait seconds to complete for reasons
	// explained in a extensive comment in readQueueHandler (inside client.go).

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
