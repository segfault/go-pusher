package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"time"
)

type client struct {
	ws                 *websocket.Conn
	Events             chan *Event
	Stop               chan bool
	ConnError          chan error
	PusherError        chan *Event
	subscribedChannels *subscribedChannels
	binders            map[string]chan *Event
	running            bool
}

// heartbeat send a ping frame to server each - TODO reconnect on disconnect
func (c *client) heartbeat() {
	for c.running {
		websocket.Message.Send(c.ws, `{"event":"pusher:ping","data":"{}"}`)
		time.Sleep(HEARTBEAT_RATE * time.Second)
	}
}

// listen to Pusher server and process/dispatch recieved events
func (c *client) listen() {
	for {
		var event Event
		err := websocket.JSON.Receive(c.ws, &event)
		if err != nil {
			c.running = false
			c.ConnError <- err
			close(c.ConnError)
			return
		} else {
			//log.Println(event)
			switch event.Event {
			case "pusher:ping":
				websocket.Message.Send(c.ws, `{"event":"pusher:pong","data":"{}"}`)
			case "pusher:pong":
			case "pusher:error":
				c.PusherError <- &event
			default:
				_, ok := c.binders[event.Event]
				if ok {
					c.binders[event.Event] <- &event
				}
			}
		}
	}
}

// Subsribe to a channel
func (c *client) Subscribe(channel string) (err error) {
	// Already subscribed ?
	if c.subscribedChannels.contains(channel) {
		err = errors.New(fmt.Sprintf("Channel %s already subscribed", channel))
		return
	}
	err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":{"channel":"%s"}}`, channel))
	if err != nil {
		return
	}
	err = c.subscribedChannels.add(channel)
	return
}

// Unsubscribe from a channel
func (c *client) Unsubscribe(channel string) (err error) {
	// subscribed ?
	if !c.subscribedChannels.contains(channel) {
		err = errors.New(fmt.Sprintf("Client isn't subscrived to %s", channel))
		return
	}
	err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:unsubscribe","data":{"channel":"%s"}}`, channel))
	if err != nil {
		return
	}
	// Remove channel from subscribedChannels slice
	c.subscribedChannels.remove(channel)
	return
}

// Bind an event
func (c *client) Bind(evt string) (dataChannel chan *Event, err error) {
	// Already binded
	_, ok := c.binders[evt]
	if ok {
		err = errors.New(fmt.Sprintf("Event %s already binded", evt))
		return
	}
	// New data channel
	dataChannel = make(chan *Event, EVENT_CHANNEL_BUFF_SIZE)
	c.binders[evt] = dataChannel
	return
}

// Unbind a event
func (c *client) Unbind(evt string) {
	delete(c.binders, evt)
}

func NewCustomClient(appKey, host, scheme string) (*client, error) {
	origin := "http://localhost/"
	url := scheme + "://" + host + "/app/" + appKey + "?protocol=" + PROTOCOL_VERSION
	ws, err := websocket.Dial(url, "", origin)
	if err != nil {
		return nil, err
	}
	var resp = make([]byte, 11000) // Pusher max message size is 10KB
	n, err := ws.Read(resp)
	if err != nil {
		return nil, err
	}
	var event Event
	err = json.Unmarshal(resp[0:n], &event)
	if err != nil {
		return nil, err
	}
	switch event.Event {
	case "pusher:error":
		var data eventError
		err = json.Unmarshal([]byte(event.Data), &data)
		if err != nil {
			return nil, err
		}
		err = errors.New(fmt.Sprintf("Pusher return error : code : %d, message %s", data.code, data.message))
		return nil, err
	case "pusher:connection_established":
		sChannels := new(subscribedChannels)
		sChannels.channels = make([]string, 0)
		pClient := client{
			ws:                 ws,
			Events:             make(chan *Event, EVENT_CHANNEL_BUFF_SIZE),
			Stop:               make(chan bool),
			ConnError:          make(chan error),
			PusherError:        make(chan *Event),
			subscribedChannels: sChannels,
			binders:            make(map[string]chan *Event),
		}
		go pClient.heartbeat()
		go pClient.listen()
		return &pClient, nil
	}
	return nil, errors.New("Ooooops something wrong happen")
}

// NewClient initialize & return a Pusher client
func NewClient(appKey string) (*client, error) {
	return NewCustomClient(appKey, "ws.pusherapp.com:443", "wss")
}
