package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// eventHub is a minimal fan-out bus for server-sent events. Slow subscribers
// drop events instead of blocking.
//
// A subscriber carries the account it belongs to, because an event may be
// ADDRESSED at a workspace. That matters more than it looks: the hub reaches
// every open browser on the instance, so anything with content in it — a page
// id, a title, the fact that something is happening at all — would otherwise go
// to people who cannot see the page. The content-free "pages" event was safe
// only by having nothing to leak; anything richer has to be filtered here.
type eventHub struct {
	mu   sync.Mutex
	subs map[chan string]string // channel -> user id
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[chan string]string{}}
}

func (h *eventHub) subscribe(userID string) chan string {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.subs[ch] = userID
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// broadcast reaches everyone. Only for events that carry no content.
func (h *eventHub) broadcast(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// broadcastTo reaches only the accounts `allowed` says may see it. The
// membership test runs once per subscriber rather than per event, and outside
// the hub's lock is not possible — so keep `allowed` cheap (isMember is a
// single indexed lookup).
func (h *eventHub) broadcastTo(event string, allowed func(userID string) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, uid := range h.subs {
		if !allowed(uid) {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

// pagesChanged tells all clients the page tree metadata changed. Deliberately
// content-free: it reaches every browser on the instance.
func (s *Server) pagesChanged() {
	s.events.broadcast(`{"type":"pages"}`)
}

// rowsChanged tells the members of a workspace that one database's rows moved.
//
// It names the database, which is why it is addressed rather than broadcast.
// The alternative — a content-free signal — was tried and is why an open board
// does not refresh today: every event would have to reload every row of every
// open database, and a database with 50k rows re-crawled itself whenever
// anybody renamed anything anywhere. Naming the collection lets a view reload
// only when it is the one that changed.
func (s *Server) rowsChanged(collectionID string) {
	if collectionID == "" {
		return
	}
	ws := s.pageWorkspace(collectionID)
	if ws == "" {
		return
	}
	b, err := json.Marshal(map[string]string{"type": "rows", "collection": collectionID})
	if err != nil {
		return
	}
	s.events.broadcastTo(string(b), func(uid string) bool { return s.isMember(uid, ws) })
}

// rowChanged is rowsChanged for a page that MIGHT be a database row. Callers
// that touch a page do not know or care whether it is one, so the check lives
// here rather than at each of them.
func (s *Server) rowChanged(pageID string) {
	var parent string
	if s.db.QueryRow(`SELECT COALESCE(parent_id, '') FROM pages WHERE id = ?`, pageID).Scan(&parent) != nil || parent == "" {
		return
	}
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM collections WHERE page_id = ?`, parent).Scan(&n) != nil || n == 0 {
		return
	}
	s.rowsChanged(parent)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		httpError(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.events.subscribe(requestUser(r).ID)
	defer s.events.unsubscribe(ch)

	fmt.Fprintf(w, "data: {\"type\":\"hello\",\"version\":%q}\n\n", Version)
	fl.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", ev)
			fl.Flush()
		case <-keepalive.C:
			// A real event rather than an SSE comment (": keepalive").
			//
			// A comment keeps the socket warm, which is what it was for, but the
			// browser never hands it to the page — so JavaScript cannot tell a
			// quiet workspace from a connection that died half an hour ago. That
			// is the whole of the "my changes only show up after I restart the
			// app" complaint: the stream was gone and nothing could know.
			//
			// As a message, silence becomes measurable: a client that has heard
			// nothing for longer than this interval knows to reconnect and
			// refetch. Older clients ignore a type they do not recognise.
			fmt.Fprint(w, "data: {\"type\":\"ping\"}\n\n")
			fl.Flush()
		}
	}
}
