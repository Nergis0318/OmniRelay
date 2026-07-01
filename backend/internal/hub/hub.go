package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	pingInterval = 30 * time.Second
	pongWait     = 60 * time.Second
	writeWait    = 10 * time.Second
)

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Conn represents a single WebSocket connection tied to one user.
type Conn struct {
	hub    *Hub
	userID int64
	done   chan struct{}
	mu     sync.Mutex
	events chan []byte
	once   sync.Once
}

func (c *Conn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *Conn) Send(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	case c.events <- payload:
		return nil
	}
}

func (c *Conn) Ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	default:
		return nil
	}
}

// Hub maintains active connections per user and routes events.
type Hub struct {
	mu          sync.Mutex
	connections map[int64][]*Conn
}

func New() *Hub {
	return &Hub{connections: make(map[int64][]*Conn)}
}

// Register adds a Conn to the hub for the given userID.
func (h *Hub) Register(userID int64) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &Conn{
		hub:    h,
		userID: userID,
		done:   make(chan struct{}),
		events: make(chan []byte, 16),
	}
	h.connections[userID] = append(h.connections[userID], c)
	return c
}

// Unregister removes a Conn from the hub.
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.connections[c.userID]
	for i, conn := range conns {
		if conn == c {
			h.connections[c.userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(h.connections[c.userID]) == 0 {
		delete(h.connections, c.userID)
	}
}

// Broadcast sends an event to all connections for the given userID.
// Failed writes are silently dropped; cleanup happens on next write attempt.
func (h *Hub) Broadcast(userID int64, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := h.connections[userID]
	var dead []*Conn
	for _, c := range conns {
		c.mu.Lock()
		select {
		case <-c.done:
			c.mu.Unlock()
			dead = append(dead, c)
			continue
		default:
		}
		select {
		case c.events <- data:
			c.mu.Unlock()
		default:
			close(c.done)
			c.mu.Unlock()
			dead = append(dead, c)
		}
	}
	h.mu.Unlock()
	for _, c := range dead {
		h.Unregister(c)
	}
}

// ReadPump handles incoming messages (pings/close) from the client.
func (c *Conn) ReadPump(conn *websocket.Conn) {
	defer func() {
		c.once.Do(func() { close(c.done) })
		c.hub.Unregister(c)
		conn.Close()
	}()
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

// WritePump sends queued events and periodic pings to the WebSocket.
// Closes the connection when done is signaled or a write fails.
func (c *Conn) WritePump(conn *websocket.Conn) {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()
	for {
		select {
		case <-c.done:
			conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait)); err != nil {
				return
			}
		case payload := <-c.events:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				c.once.Do(func() { close(c.done) })
				c.hub.Unregister(c)
				return
			}
		}
	}
}
