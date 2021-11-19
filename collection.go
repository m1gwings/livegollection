package livegollection

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/m1gwings/livegollection/pool"
)

type Item interface {
	GetID() string
}

type Collection interface {
	GetAll() ([]Item, error)
	Get(ID string) (Item, error)
	Create(Item) (Item, error)
	Update(Item) error
	Delete(ID string) error
}

type LiveCollection struct {
	coll   Collection
	pool   *pool.Pool
	logger *log.Logger
}

func NewLiveCollection(ctx context.Context, coll Collection, logger *log.Logger) *LiveCollection {
	lC := &LiveCollection{
		coll:   coll,
		pool:   pool.NewPool(logger),
		logger: logger,
	}

	go lC.updatesHandler(ctx)

	return lC
}

// TODO: Find a more elegant way.
func (lC *LiveCollection) logError(err error) {
	if lC.logger == nil {
		return
	}

	lC.logger.Printf("livegollection: %v\n", err)
}

func (lC *LiveCollection) Join(w http.ResponseWriter, r *http.Request) {
	initialItems, err := lC.coll.GetAll()
	if err != nil {
		lC.logError(fmt.Errorf("error in Join from coll.GetAll: %v", err))
	}

	initialMessages := make([][]byte, 0, len(initialItems))
	for _, item := range initialItems {
		var updMess updateMess
		updMess.Method = createMethodString
		updMess.ID = item.GetID()
		updMess.Item = item.(DummyItem)
		message, err := json.Marshal(updMess)
		if err != nil {
			lC.logError(err)
			continue
		}
		initialMessages = append(initialMessages, message)
	}

	lC.pool.AddToPool(w, r, websocket.TextMessage, initialMessages)
}

type updateMethod string

const (
	createMethodString updateMethod = "CREATE"
	updateMethodString updateMethod = "UPDATE"
	deleteMethodString updateMethod = "DELETE"
)

func isValidUpdateMethod(method updateMethod) bool {
	return method == createMethodString || method == updateMethodString || method == deleteMethodString
}

type updateMess struct {
	Method updateMethod `json:"method"`
	ID     string       `json:"id,omitempty"`
	Item   DummyItem    `json:"item"`
}

func (lC *LiveCollection) updatesHandler(ctx context.Context) {
	defer lC.pool.CloseAll()

	for {
		select {
		case updateBody := <-lC.pool.ReadQueue():
			var updMess updateMess
			err := json.Unmarshal(updateBody, &updMess)
			if err != nil {
				lC.logError(err)
				continue
			}
			if !isValidUpdateMethod(updMess.Method) {
				continue
			}

			switch updMess.Method {
			case createMethodString:
				item, err := lC.coll.Create(updMess.Item)
				if err != nil {
					lC.logError(err)
					continue
				}

				updMess.ID = item.GetID()
				updMess.Item = item.(DummyItem)

				toSendData, err := json.Marshal(updMess)
				if err != nil {
					lC.logError(err)
					continue
				}

				lC.pool.SendMessageToAll(websocket.TextMessage, toSendData)

			case updateMethodString:
				err := lC.coll.Update(updMess.Item)
				if err != nil {
					lC.logError(err)
					continue
				}

				toSendData, err := json.Marshal(updMess)
				if err != nil {
					lC.logError(err)
					continue
				}

				lC.pool.SendMessageToAll(websocket.TextMessage, toSendData)

			case deleteMethodString:
				err := lC.coll.Delete(updMess.ID)
				if err != nil {
					lC.logError(err)
					continue
				}

				toSendData, err := json.Marshal(updMess)
				if err != nil {
					lC.logError(err)
					continue
				}

				lC.pool.SendMessageToAll(websocket.TextMessage, toSendData)
			}

		case <-ctx.Done():
			return
		}
	}
}
