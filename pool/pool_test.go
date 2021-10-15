package pool

import (
	"bytes"
	"fmt"
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

// newTestServer returns an http test server after having set its handler,
// which adds the client who has made the GET requests to the pool.
func newTestServer(pool *Pool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pool.AddToPool(w, r, 0, nil)
	}))
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

// readLoop is needed in order to process control messages received by c.
func readLoop(c *websocket.Conn) {
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			c.Close()
			return
		}
	}
}

// waitForClose waits until each connection object in conns recives a close message from the pool.
// It is important to call waitForClose at the end of each test (after pool.CloseAll() and
// before testSrv.Close()) because otherwise, if an error in the pool occurs after the test has finished,
// the test logger associated with the pool will try to access t.Error through poolErrorsWriter,
// ending with a panic.
// With waitForClose we wait until both readQueueHandler and writeQueueHandler have finished, since the
// close message is sent at the end of closeHandler, so the only error that could lead to the problem
// described above is the one generated by conn.Close, but for now waitForClose seems the best compromise.
func waitForClose(conns []*websocket.Conn) {
	var wg sync.WaitGroup

	for _, c := range conns {
		wg.Add(1)
		go func(c *websocket.Conn) {
			readLoop(c)
			wg.Done()
		}(c)
	}

	wg.Wait()
}

func TestAddToPool(t *testing.T) {
	pool := newTestPool(t)

	const initialMessN = 10

	initialMessages := make([][]byte, 0, initialMessN)
	for i := 0; i < initialMessN; i++ {
		initialMessages = append(initialMessages, []byte(fmt.Sprint(i)))
	}

	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pool.AddToPool(w, r, websocket.TextMessage, initialMessages)
	}))

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	defer func() {
		pool.CloseAll()
		waitForClose(conns)
		testSrv.Close()
	}()

	for j, c := range conns {
		for i := 0; i < initialMessN; i++ {
			messageType, data, err := c.ReadMessage()
			if err != nil {
				t.Errorf("error while reading message from client %d: %v", i, err)
				continue
			}

			if messageType != websocket.TextMessage {
				t.Errorf("unexpected message type for client %d, got %d, expected %d",
					j, messageType, websocket.TextMessage)
			}

			if !bytes.Equal(data, initialMessages[i]) {
				t.Errorf("unexpected message body for client %d, got %v, expected %v",
					j, data, initialMessages[i])
			}
		}
	}
}

func TestCloseAll(t *testing.T) {
	pool := newTestPool(t)

	testSrv := newTestServer(pool)
	defer testSrv.Close()

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
		t.FailNow()
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

	testSrv := newTestServer(pool)

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	defer func() {
		pool.CloseAll()
		waitForClose(conns)
		testSrv.Close()
	}()

	const messText = "hello!"

	pool.SendMessageToAll(websocket.TextMessage, []byte(messText))

	for i, c := range conns {
		messageType, p, err := c.ReadMessage()
		if err != nil {
			t.Errorf("error while reading message from client %d: %v", i, err)
			continue
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

func TestOrderOfArrival(t *testing.T) {
	pool := newTestPool(t)

	testSrv := newTestServer(pool)

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	defer func() {
		pool.CloseAll()
		waitForClose(conns)
		testSrv.Close()
	}()

	const messagesNum = 100
	messages := make([][]byte, messagesNum)
	for i := 0; i < messagesNum; i++ {
		messages[i] = []byte(fmt.Sprint(i))
		pool.SendMessageToAll(websocket.TextMessage, messages[i])
	}

	for j, c := range conns {
		for i := 0; i < messagesNum; i++ {
			messageType, data, err := c.ReadMessage()
			if err != nil {
				t.Error(err)
				continue
			}

			if messageType != websocket.TextMessage {
				t.Errorf("unexpected message type for client %d, got %d, expected %d",
					j, messageType, websocket.TextMessage)
			}

			if !bytes.Equal(data, messages[i]) {
				t.Errorf("unexpected message body for client %d, got %v, expected %v",
					j, data, messages[i])
			}
		}
	}
}

func TestReadQueue(t *testing.T) {
	pool := newTestPool(t)

	testSrv := newTestServer(pool)

	const connsNum = 10

	conns, err := populate(switchProtocol(testSrv.URL), connsNum)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	defer func() {
		pool.CloseAll()
		waitForClose(conns)
		testSrv.Close()
	}()

	const messText = "hello!"

	for _, c := range conns {
		if err := c.WriteMessage(websocket.TextMessage, []byte(messText)); err != nil {
			t.Error(err)
			t.FailNow()
		}
	}

	for i := 0; i < connsNum; i++ {
		mess := <-pool.ReadQueue()
		if string(mess) != messText {
			t.Errorf("unexpected message body, got %s, expected %s", string(mess), messText)
		}
	}
}
