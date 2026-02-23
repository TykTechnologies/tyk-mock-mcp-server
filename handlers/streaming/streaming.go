package streaming

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// StreamHandler serves a configurable SSE event stream for testing gateway
// SSE proxy behaviour (timeout handling, Content-Type detection, flushing).
//
// Query parameters:
//   - events:   number of events to send (default 10)
//   - delay_ms: milliseconds between events (default 1000)
//   - charset:  if set, appends "; charset=<value>" to Content-Type
func StreamHandler(w http.ResponseWriter, r *http.Request) {
	events := intParam(r, "events", 10)
	delayMs := intParam(r, "delay_ms", 1000)
	charset := r.URL.Query().Get("charset")

	ct := "text/event-stream"
	if charset != "" {
		ct = fmt.Sprintf("text/event-stream; charset=%s", charset)
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for i := 1; i <= events; i++ {
		select {
		case <-r.Context().Done():
			log.Printf("SSE stream: client disconnected after %d/%d events", i-1, events)
			return
		default:
		}

		fmt.Fprintf(w, "id: %d\nevent: message\ndata: {\"seq\":%d,\"total\":%d,\"timestamp\":\"%s\"}\n\n",
			i, i, events, time.Now().UTC().Format(time.RFC3339Nano))
		flusher.Flush()

		if i < events {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	fmt.Fprintf(w, "event: done\ndata: {\"total_events\":%d}\n\n", events)
	flusher.Flush()
}

// CrashHandler serves N SSE events then abruptly closes the TCP connection
// to simulate an upstream crash. No completion or error event is sent.
//
// Query parameters:
//   - events_before_crash: events to send before crashing (default 3)
//   - delay_ms:            milliseconds between events (default 1000)
//   - charset:             if set, appends "; charset=<value>" to Content-Type
func CrashHandler(w http.ResponseWriter, r *http.Request) {
	events := intParam(r, "events_before_crash", 3)
	delayMs := intParam(r, "delay_ms", 1000)
	charset := r.URL.Query().Get("charset")

	ct := "text/event-stream"
	if charset != "" {
		ct = fmt.Sprintf("text/event-stream; charset=%s", charset)
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for i := 1; i <= events; i++ {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		fmt.Fprintf(w, "id: %d\nevent: message\ndata: {\"seq\":%d,\"crash_after\":%d}\n\n",
			i, i, events)
		flusher.Flush()

		if i < events {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	// Simulate crash: hijack the connection and close the raw TCP socket.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Println("SSE crash: hijacking not supported, returning abruptly without completion event")
		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("SSE crash: hijack error: %v", err)
		return
	}

	conn.Close()
	log.Printf("SSE crash: connection closed abruptly after %d events", events)
}

func intParam(r *http.Request, name string, defaultVal int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
