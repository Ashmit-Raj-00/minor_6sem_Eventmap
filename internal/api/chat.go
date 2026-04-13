package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"eventmap/internal/store"
)

type chatHub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.ChatMessage]struct{} // eventID -> subs
}

func newChatHub() *chatHub {
	return &chatHub{subs: map[string]map[chan store.ChatMessage]struct{}{}}
}

func (h *chatHub) Subscribe(eventID string) (ch chan store.ChatMessage, unsubscribe func()) {
	ch = make(chan store.ChatMessage, 32)
	h.mu.Lock()
	if h.subs[eventID] == nil {
		h.subs[eventID] = map[chan store.ChatMessage]struct{}{}
	}
	h.subs[eventID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if mm := h.subs[eventID]; mm != nil {
			delete(mm, ch)
			if len(mm) == 0 {
				delete(h.subs, eventID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
}

func (h *chatHub) Broadcast(msg store.ChatMessage) {
	h.mu.Lock()
	mm := h.subs[msg.EventID]
	for ch := range mm {
		select {
		case ch <- msg:
		default:
			// Drop rather than blocking; clients can refetch history.
		}
	}
	h.mu.Unlock()
}

func (h *handlers) handleChatStream(w http.ResponseWriter, r *http.Request, eventID, userID string) {
	// Basic SSE stream; clients should also poll /chat for history if needed.
	msgs, err := h.st.ListChatMessages(eventID, userID, 50)
	if err != nil {
		status := http.StatusBadRequest
		if err == store.ErrForbidden {
			status = http.StatusForbidden
		} else if err == store.ErrNotFound {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "stream_unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial snapshot.
	for _, m := range msgs {
		writeSSE(w, "message", m)
	}
	flusher.Flush()

	ch, unsubscribe := h.chat.Subscribe(eventID)
	defer unsubscribe()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			writeSSE(w, "message", msg)
			flusher.Flush()
		case <-keepalive.C:
			_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, v any) {
	b, _ := json.Marshal(v)
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
}

