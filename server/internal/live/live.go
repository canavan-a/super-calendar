// Package live tells open apps when something they show has changed, so they
// refetch it: one WebSocket per open app (GET /live), and a hub that sends a
// small JSON message to all of a user's connections. Messages say what
// changed and where, never the data itself; the app reloads through the
// normal API, so access checks stay in one place.
package live

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message is one notification. Type is:
//
//	hello      sent on connect: refetch everything (anything may have been missed)
//	changed    something in CalendarID changed (Area, and ItemID when it's one item)
//	calendars  your list of calendars changed (joined, left, made one)
//	inbox      your Inbox changed (an invite arrived or was cancelled)
type Message struct {
	Type       string `json:"type"`
	CalendarID string `json:"calendarId,omitempty"`
	// events, training, cycle, calendar (name, colour, settings), members, invites
	Area   string `json:"area,omitempty"`
	ItemID string `json:"itemId,omitempty"`
	By     string `json:"by,omitempty"`     // user id of who made the change
	ByName string `json:"byName,omitempty"` // their display name
	// Origin is the X-Live-Client id of the app that made the change, so it
	// can ignore its own echo (it already shows the change).
	Origin string `json:"origin,omitempty"`
}

const (
	pingEvery = 30 * time.Second // under typical proxy idle timeouts
	readWait  = 70 * time.Second // two missed pongs and the connection is dropped
	writeWait = 10 * time.Second
	sendQueue = 64
)

type conn struct {
	ws   *websocket.Conn
	send chan []byte
}

// Hub holds every open connection, by user.
type Hub struct {
	mu    sync.Mutex
	conns map[string]map[*conn]struct{}
}

func NewHub() *Hub { return &Hub{conns: map[string]map[*conn]struct{}{}} }

// Send delivers m to every open app of each user in userIDs.
func (h *Hub) Send(userIDs []string, m Message) {
	payload, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range userIDs {
		for c := range h.conns[id] {
			select {
			case c.send <- payload:
			default:
				// A stuck reader: drop the message rather than block everyone.
				// It refetches everything when it reconnects anyway.
			}
		}
	}
}

func (h *Hub) add(userID string, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == nil {
		h.conns[userID] = map[*conn]struct{}{}
	}
	h.conns[userID][c] = struct{}{}
}

func (h *Hub) remove(userID string, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns[userID], c)
	if len(h.conns[userID]) == 0 {
		delete(h.conns, userID)
	}
}

// Connections is how many apps are connected, for tests and logs.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, cs := range h.conns {
		n += len(cs)
	}
	return n
}

var upgrader = websocket.Upgrader{
	// Apps authenticate with their bearer token (RequireAuth runs first), so
	// the origin of the request doesn't matter.
	CheckOrigin: func(*http.Request) bool { return true },
}

// Serve upgrades an authenticated request to the live socket for userID.
// The app sends nothing; reads only keep the connection's liveness checked.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID string) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &conn{ws: ws, send: make(chan []byte, sendQueue)}
	h.add(userID, c)
	hello, _ := json.Marshal(Message{Type: "hello"})
	c.send <- hello

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(pingEvery)
		defer ticker.Stop()
		for {
			select {
			case payload, ok := <-c.send:
				if !ok {
					return
				}
				_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.TextMessage, payload); err != nil {
					return
				}
			case <-ticker.C:
				if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
					return
				}
			}
		}
	}()

	_ = ws.SetReadDeadline(time.Now().Add(readWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(readWait)) })
	ws.SetReadLimit(1024)
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
				log.Printf("live: %v", err)
			}
			break
		}
	}
	// Once removed (under the hub's lock) nothing sends to c any more, so its
	// queue can be closed, which stops the writer.
	h.remove(userID, c)
	close(c.send)
	<-done
	_ = ws.Close()
}
