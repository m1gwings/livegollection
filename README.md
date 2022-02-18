# livegollection
**livegollection** is a Golang library for live data synchronization between backend and frontend of a custom user-implemented collection. It's aimed for web applications since it works over websockets.

To interact with **livegollection** from client-side JavaScript/TypeScript check out **[livegollection-client](https://github.com/m1gwings/livegollection-client)**.
# Install
To install **livegollection** you just need to:
```bash
go get github.com/m1gwings/livegollection
```
NOTE: **livegollection** makes use of type parameters so you need go1.18 or above in order to use it (at the time of writing go1.18 has not been released yet but there is a beta version and a release candidate available).
# How to use
## Define the user-implemented collection
We need to define the user-implemented collection that is going to be synchronized between the server and multiple web-clients with live updates.

This collection must satisfy the livegollection.Collection interface which requires the following methods: All, Item, Create, Update, Delete.
This is a simple template that you can use for implementing such a collection **(you need to substitute IdType with a concrete type for items' id)**:
```bash
cat mycollection/mycollection.go
```
```go
package mycollection

// IMPORTANT: export all the fields of MyItem, livegollection internally uses json.Marshal.
type MyItem struct {
	Id       IdType         `json:"id,omitempty"`
	FieldOne TypeOfFieldOne `json:"fieldOne,omitempty"`
    FieldTwo TypeOfFIeldTwo `json:"fieldTwo,omitempty"`
    // ...
}

func (myItem MyItem) ID() IdType {
    return myItem.Id
}

type MyCollection struct {
    // ...
}

// NewMyCollection creates and initialize an instance of MyCollection.
func NewMyCollection() (*MyCollection, error) {
    
}

// All retreives all the items in MyCollecttion and returns them in a slice.
func (myColl *MyCollection) All() ([]MyItem, error) {

}

// Item retreives the item with the given ID from MyCollection and returns it.
func (myColl *MyCollection) Item(ID IdType) (MyItem, error) {

}

// Create adds the given item to the collection and returns it AFTER having set its id.
func (myColl *MyCollection) Create(myItem MyItem) (MyItem, error) {
    // ...
    // Set item id
    myItem.Id = // ...
    // ...
    return myItem, nil
}

// Update updates the given item in the collection.
func (myColl *MyCollection) Update(myItem MyItem) error {

}

// Delete deletes the given item from the collection.
func (myColl *MyCollection) Delete(ID IdType) error {

}
```
## Implement server executable
It's time to implement our backend logic and add to our server the **livegollection** handler:
```bash
cat server/main.go
```
```go
package main

import (
    "github.com/m1gwings/livegollection"
    "module-name/mycollection"
)

func main() {
	// ...

	myColl, err := mycollection.NewMyCollection()
	if err != nil {
		log.Fatal(fmt.Errorf("error when creating MyCollection: %v", err))
	}

	// When we create the LiveGollection we need to specify the type parameters for items' id and items themselves.
	liveGoll := livegollection.NewLiveGollection[IdType, mycollection.MyItem](context.TODO(), myColl, log.Default())

	// After we created liveGoll, to enable it we just need to register a route handled by liveGoll.Join.
	http.HandleFunc("/livegollection", liveGoll.Join)

	log.Fatal(http.ListenAndServe("localhost:8080", nil))
}
// ...
```
As you can see, once you have implemented `MyCollection` you just need to register an HTTP handler and server-side **livegollection** is ready to use.
## Further steps
As anticipated to interact with server-side **livegollection** from client-side JavaScript/TypeScript check out **[livegollection-client](https://github.com/m1gwings/livegollection-client)**.
# Example app
If you want a concrete example of usage of **livegollection** and **livegollection-client** check out this [example app](https://github.com/m1gwings/livegollection-example-app).