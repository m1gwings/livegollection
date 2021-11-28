package livegollection

import (
	"fmt"
	"strconv"
)

type DummyItem struct {
	Id     string `json:"id"`
	String string `json:"string,omitempty"`
	Num    int    `json:"num,omitempty"`
}

func (d DummyItem) ID() string {
	return d.Id
}

type DummyCollection struct {
	data map[string]DummyItem
	i    int64
}

func NewDummyCollection() *DummyCollection {
	return &DummyCollection{data: make(map[string]DummyItem), i: 1}
}

func (c *DummyCollection) All() ([]Item, error) {
	items := make([]Item, 0, len(c.data))
	for _, d := range c.data {
		items = append(items, d)
	}

	return items, nil
}

func (c *DummyCollection) Item(ID string) (Item, error) {
	d, ok := c.data[ID]
	if !ok {
		return DummyItem{}, fmt.Errorf("there is no item with this ID: %s", ID)
	}

	return d, nil
}

func (c *DummyCollection) Create(item Item) (Item, error) {
	d, ok := item.(DummyItem)
	if !ok {
		return nil, fmt.Errorf("can't convert Item to DummyItem")
	}

	newID := strconv.FormatInt(c.i, 16)
	d.Id = newID

	c.i++
	c.data[newID] = d

	return d, nil
}

func (c *DummyCollection) Update(item Item) error {
	d, ok := item.(DummyItem)
	if !ok {
		return fmt.Errorf("can't convert Item to DummyItem")
	}

	_, ok = c.data[d.Id]
	if !ok {
		return fmt.Errorf("the following item isn't in the collection anymore: %v", d)
	}

	c.data[d.Id] = d

	return nil
}

func (c *DummyCollection) Delete(ID string) error {
	_, ok := c.data[ID]
	if !ok {
		return fmt.Errorf("there is no item with this ID: %s", ID)
	}

	delete(c.data, ID)

	return nil
}
