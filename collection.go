/*
Package livegollection implements a library for live synchronization between backend and frontend of a custom user-implemented collection.
It's aimed for web applications since it works over websockets.
*/
package livegollection

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/m1gwings/livegollection/pool"
)

// Item is the interface that the items of the user-implemented collection (which is going to be synchronized) must satisfy.
// It wraps the ID method, which is supposed to return the item's id as a string.
type Item interface {
	ID() string
}

// Collection is the interface that the user-implemented collection (which is going to be synchronized) must satisfy.
// It wraps the following methods: All, Item, Create, Update, Delete.
// All returns all the items inside the collection (as a slice of items) or an error if something goes wrong.
// Item takes an id and returns the correspondant item or an error if something goes wrong.
// Create takes an item which will be added to the collection.
// It returns the newly added item after its id field has been set or an error if something goes wrong.
// Update takes an item which will be updated inside the collection and returns an error if something goes wrong.
// Delete takes an id and deletes the correspondant item from the collection. It returns an error if something goes wrong.
type Collection[T Item] interface {
	All() ([]T, error)
	Item(ID string) (T, error)
	Create(T) (T, error)
	Update(T) error
	Delete(ID string) error
}

// LiveGollection represents the instance of the livegollection server.
// It handles the pool of clients by sending and receiving live updates and
// calls the appropriate methods on the underlying user-implemented collection to keep it synchronized.
type LiveGollection[T Item] struct {
	coll   Collection[T]
	pool   *pool.Pool
	logger *log.Logger
	mu     sync.Mutex
}

// NewLiveGollection returns a pointer to a new LiveGollection instance after having set it properly.
// It takes the following parameters: ctx, coll, logger.
// ctx is the Context object that can be used to terminate the LiveGollection and close the connections with all the clients in the pool.
// Pass context.TODO() if it's unclear which context to use.
// coll is the istance of the underlying user-implemented collection that will be synchronized.
// logger is where all the errors and messages will be reported.
// Pass nil if logging is not needed.
func NewLiveGollection[T Item](ctx context.Context, coll Collection[T], logger *log.Logger) *LiveGollection[T] {
	lG := &LiveGollection[T]{
		coll:   coll,
		pool:   pool.NewPool(logger),
		logger: logger,
	}

	go lG.updatesHandler(ctx)

	return lG
}

// logError, after having checked that the logger has been set,
// logs errors, adding to them the special prefix: "livegollection: ".
func (lG *LiveGollection[T]) logError(err error) {
	if lG.logger == nil {
		return
	}

	lG.logger.Printf("livegollection: %v\n", err)
}

// Join is an http.HandlerFunc.
// The proper way to set the websocket server-side handler of livegollection is to
// add a route to this function with, for example, http.HandleFunc("route/to/livegollection", liveGoll.Join) .
func (lG *LiveGollection[T]) Join(w http.ResponseWriter, r *http.Request) {
	// We need to lock the mutex because otherwise we would miss the updates received
	// between the call to All and the call to AddToPool.
	lG.mu.Lock()
	defer lG.mu.Unlock()

	initialItems, err := lG.coll.All()
	if err != nil {
		lG.logError(fmt.Errorf("error in Join from coll.GetAll: %v", err))
	}

	initialMessages := make([][]byte, 0, len(initialItems))
	for _, item := range initialItems {
		var updMess updateMess[T]
		updMess.Method = createMethodString
		updMess.ID = item.ID()
		updMess.Item = item
		message, err := json.Marshal(updMess)
		if err != nil {
			lG.logError(err)
			continue
		}
		initialMessages = append(initialMessages, message)
	}

	lG.pool.AddToPool(w, r, websocket.TextMessage, initialMessages)
}

type updateMethod string

// Method strings used in update messages to specify the type of event of the update.
const (
	createMethodString updateMethod = "CREATE"
	updateMethodString updateMethod = "UPDATE"
	deleteMethodString updateMethod = "DELETE"
)

func isValidUpdateMethod(method updateMethod) bool {
	return method == createMethodString || method == updateMethodString || method == deleteMethodString
}

// updateMess represents the live update messages exchanged with the clients.
type updateMess[T Item] struct {
	Method updateMethod `json:"method"`
	ID     string       `json:"id,omitempty"`   // CREATE update message hasn't an ID
	Item   T            `json:"item,omitempty"` // DELETE update message hasn't an item
}

// updatesHandler listens for incoming update messages from clients.
// When it receives one it calls the appropriate method on the underlying user-implemented collection
// and then dispatches the message to all the clients in the pool.
func (lG *LiveGollection[T]) updatesHandler(ctx context.Context) {
	defer lG.pool.CloseAll()

	for {
		select {
		case updateBody := <-lG.pool.ReadQueue():
			// We need to lock the mutex to prevent sending updates while we
			// are adding a client to the pool since the new client wouldn't receive them.
			lG.mu.Lock()

			var updMess updateMess[T]
			err := json.Unmarshal(updateBody, &updMess)
			if err != nil {
				lG.logError(err)
				continue
			}
			if !isValidUpdateMethod(updMess.Method) {
				continue
			}

			switch updMess.Method {
			case createMethodString:
				item, err := lG.coll.Create(updMess.Item)
				if err != nil {
					lG.logError(err)
					continue
				}

				updMess.ID = item.ID()
				updMess.Item = item

			case updateMethodString:
				err := lG.coll.Update(updMess.Item)
				if err != nil {
					lG.logError(err)
					continue
				}

			case deleteMethodString:
				err := lG.coll.Delete(updMess.ID)
				if err != nil {
					lG.logError(err)
					continue
				}
			}

			toSendData, err := json.Marshal(updMess)
			if err != nil {
				lG.logError(err)
				continue
			}

			lG.pool.SendMessageToAll(websocket.TextMessage, toSendData)

			lG.mu.Unlock()

		case <-ctx.Done():
			return
		}
	}
}
