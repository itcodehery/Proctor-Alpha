package main

import (
	"encoding/json"
	"log"
	"net/http"
	// "sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for this demo
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	// Active subscriptions
	subs map[string]bool // "all" or "room_ID"
}

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

type Message struct {
	Type    string      `json:"type"`    // "ROOM_LIST_UPDATE", "ROOM_UPDATE"
	Payload interface{} `json:"payload"` // The data
	Target  string      `json:"target"`  // "all" or specific roomID
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			msgBytes, err := json.Marshal(message)
			if err != nil {
				log.Printf("json marshal error: %v", err)
				continue
			}

			for client := range h.clients {
				shouldSend := false
				if message.Target == "all" {
					if client.subs["all"] {
						shouldSend = true
					}
				} else {
					if client.subs[message.Target] {
						shouldSend = true
					}
				}

				// Always send to "subscribe_all" admin clients for list updates?
				// Actually, if Target is a roomID, admin watching list might not care unless it changes list metadata?
				// For simplicity:
				// - "all" targets clients subscribed to "all"
				// - "roomID" targets clients subscribed to "roomID"
				// - If a room updates, we might desire to update the list too? Handled by caller sending two messages if needed.

				if shouldSend {
					select {
					case client.send <- msgBytes:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			}
		}
	}
}

// readPump pumps messages from the websocket connection to the hub.
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		
		// Handle Subscription Messages
		var cmd struct {
			Action string `json:"action"` // "subscribe_all", "subscribe_room"
			RoomID string `json:"room_id"`
		}
		if err := json.Unmarshal(message, &cmd); err == nil {
			if cmd.Action == "subscribe_all" {
				c.subs["all"] = true
			} else if cmd.Action == "subscribe_room" && cmd.RoomID != "" {
				c.subs[cmd.RoomID] = true
			} else if cmd.Action == "unsubscribe_room" && cmd.RoomID != "" {
				delete(c.subs, cmd.RoomID)
			}
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), subs: make(map[string]bool)}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
