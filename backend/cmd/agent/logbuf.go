package main

import (
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry is one captured log line.
type LogEntry struct {
	At   time.Time `json:"at"`
	Line string    `json:"line"`
}

// ringLogger captures everything written to the standard logger into a fixed
// in-memory ring buffer, and pushes new entries to any subscribers (SSE).
type ringLogger struct {
	mu   sync.Mutex
	buf  []LogEntry
	subs map[chan LogEntry]struct{}
	out  io.Writer
}

var logBuf = &ringLogger{
	subs: map[chan LogEntry]struct{}{},
	out:  os.Stderr,
}

// AgentStatus is updated by the agent's lifecycle code and exposed via
// /api/status on the dashboard.
type AgentStatus struct {
	State        string    `json:"state"`        // starting | connecting | connected | disconnected
	LastError    string    `json:"lastError,omitempty"`
	ConnectedAt  time.Time `json:"connectedAt,omitempty"`
	LastChangeAt time.Time `json:"lastChangeAt"`
}

var (
	statusValue atomic.Value // holds AgentStatus
)

func init() {
	statusValue.Store(AgentStatus{State: "starting", LastChangeAt: time.Now()})
}

// SetStatus updates the agent's exposed state. Safe from any goroutine.
func SetStatus(state string, lastErr ...string) {
	s := AgentStatus{State: state, LastChangeAt: time.Now()}
	if prev, ok := statusValue.Load().(AgentStatus); ok {
		s.ConnectedAt = prev.ConnectedAt
	}
	if state == "connected" {
		s.ConnectedAt = time.Now()
	}
	if len(lastErr) > 0 {
		s.LastError = lastErr[0]
	}
	statusValue.Store(s)
}

func GetStatus() AgentStatus {
	if s, ok := statusValue.Load().(AgentStatus); ok {
		return s
	}
	return AgentStatus{State: "unknown"}
}

func (r *ringLogger) Write(p []byte) (int, error) {
	// Echo to original stderr so the console window still shows logs.
	n, _ := r.out.Write(p)

	// Split on newlines and store each line separately.
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return n, nil
	}
	now := time.Now()
	r.mu.Lock()
	for _, line := range strings.Split(text, "\n") {
		entry := LogEntry{At: now, Line: line}
		r.buf = append(r.buf, entry)
		if len(r.buf) > 500 {
			r.buf = r.buf[len(r.buf)-500:]
		}
		for ch := range r.subs {
			select {
			case ch <- entry:
			default: // subscriber slow — drop
			}
		}
	}
	r.mu.Unlock()
	return n, nil
}

func (r *ringLogger) Snapshot() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogEntry, len(r.buf))
	copy(out, r.buf)
	return out
}

func (r *ringLogger) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 32)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

func (r *ringLogger) Unsubscribe(ch chan LogEntry) {
	r.mu.Lock()
	delete(r.subs, ch)
	r.mu.Unlock()
	close(ch)
}
