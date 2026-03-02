package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/api/middleware"
)

// StreamRegistry is a thread-safe fan-out registry that maps accountID → set of SSE subscribers.
//
// Design: each subscriber is represented by a buffered channel. The channel is
// never closed — the SSE handler exits when the request context is cancelled.
// Unsubscribe removes the channel from the registry; subsequent Publish calls
// will no longer see it. Because the channel is never closed, there is no
// send-on-closed-channel race.
type StreamRegistry struct {
	mu          sync.Mutex
	subscribers map[string]map[chan []byte]struct{}
}

// NewStreamRegistry creates a new StreamRegistry.
func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{
		subscribers: make(map[string]map[chan []byte]struct{}),
	}
}

// Subscribe registers a new subscriber for the given account.
// Returns:
//   - ch: receive-only view of the event channel (never closed)
//   - unsubscribe: removes the subscriber from the registry; must be called exactly once
func (r *StreamRegistry) Subscribe(accountID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	r.mu.Lock()
	if r.subscribers[accountID] == nil {
		r.subscribers[accountID] = make(map[chan []byte]struct{})
	}
	r.subscribers[accountID][ch] = struct{}{}
	r.mu.Unlock()

	unsubscribe := func() {
		r.mu.Lock()
		delete(r.subscribers[accountID], ch)
		if len(r.subscribers[accountID]) == 0 {
			delete(r.subscribers, accountID)
		}
		r.mu.Unlock()
		// Do NOT close ch. The SSE handler exits via ctx.Done(); closing the
		// channel here would race with any in-flight Publish that has already
		// taken a snapshot reference to ch.
	}
	return ch, unsubscribe
}

// Publish sends a JSON payload to all active subscribers for the given account.
// Slow consumers are skipped (non-blocking). It is safe to call concurrently.
func (r *StreamRegistry) Publish(accountID string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Warn().Err(err).Str("account_id", accountID).Msg("stream: failed to marshal payload")
		return
	}

	r.mu.Lock()
	subs := r.subscribers[accountID]
	snapshot := make([]chan []byte, 0, len(subs))
	for ch := range subs {
		snapshot = append(snapshot, ch)
	}
	r.mu.Unlock()

	for _, ch := range snapshot {
		select {
		case ch <- data:
		default:
			// Buffer full — drop for slow consumer.
		}
	}
}

// handleTradeStream handles GET /api/v1/accounts/{accountId}/trades/stream.
// It opens a long-lived SSE connection and pushes trade events as they occur.
func (s *Server) handleTradeStream(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountId")
	tenantID := middleware.TenantIDFromContext(r.Context())

	// Verify account exists (optional - we allow streaming even if not yet created).
	_ = tenantID

	// Ensure the response writer supports flushing.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Register subscriber.
	ch, unsubscribe := s.streamRegistry.Subscribe(accountID)
	defer unsubscribe()

	log.Debug().Str("account_id", accountID).Msg("SSE client connected")

	// Stream events until client disconnects or context is cancelled.
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			log.Debug().Str("account_id", accountID).Msg("SSE client disconnected")
			return
		case data := <-ch:
			// Write SSE event: "data: <json>\n\n"
			_, err := fmt.Fprintf(w, "data: %s\n\n", data)
			if err != nil {
				log.Debug().Err(err).Str("account_id", accountID).Msg("SSE write error, disconnecting")
				return
			}
			flusher.Flush()
		}
	}
}
