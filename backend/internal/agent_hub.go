package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/google/uuid"
)

// AgentHub keeps track of connected agents and brokers RPC requests over their
// WebSocket connections.
type AgentHub struct {
	mu      sync.RWMutex
	agents  map[string]*AgentConn
	onEvent func(Frame)
}

type AgentConn struct {
	ID       string
	Name     string
	Version  string
	JoinedAt time.Time

	ws  *websocket.Conn
	out chan Frame

	mu      sync.Mutex
	pending map[string]chan Frame
	closed  bool
}

func NewAgentHub() *AgentHub {
	return &AgentHub{agents: map[string]*AgentConn{}}
}

func (h *AgentHub) IsOnline(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.agents[agentID]
	return ok
}

func (h *AgentHub) ListOnline() []map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]map[string]any, 0, len(h.agents))
	for id, c := range h.agents {
		out = append(out, map[string]any{
			"id":       id,
			"name":     c.Name,
			"version":  c.Version,
			"joinedAt": c.JoinedAt,
		})
	}
	return out
}

// Do sends an RPC request to the agent and waits up to `timeout` for the reply.
func (h *AgentHub) Do(ctx context.Context, agentID string, req Frame, timeout time.Duration) (Frame, error) {
	h.mu.RLock()
	conn := h.agents[agentID]
	h.mu.RUnlock()
	if conn == nil {
		return Frame{}, errors.New("agent not connected: " + agentID)
	}

	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	req.Type = "req"

	ch := make(chan Frame, 1)
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return Frame{}, errors.New("agent connection closed")
	}
	conn.pending[req.ID] = ch
	conn.mu.Unlock()

	defer func() {
		conn.mu.Lock()
		delete(conn.pending, req.ID)
		conn.mu.Unlock()
	}()

	select {
	case conn.out <- req:
	case <-time.After(2 * time.Second):
		return Frame{}, errors.New("send queue full")
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return Frame{}, errors.New("agent response timeout")
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

// Handle is the WebSocket handler. Mount on /agent/ws.
func (h *AgentHub) Handle(c *websocket.Conn) {
	agentID := c.Query("id")
	token := c.Query("token")
	expected := getenv("AGENT_TOKEN", "")
	if expected != "" && token != expected {
		log.Printf("[agent-hub] reject %s: bad token", agentID)
		_ = c.WriteJSON(Frame{Type: "error", Error: "invalid token"})
		_ = c.Close()
		return
	}
	if agentID == "" {
		agentID = "agent-" + uuid.NewString()[:8]
	}

	ac := &AgentConn{
		ID:       agentID,
		ws:       c,
		out:      make(chan Frame, 32),
		pending:  map[string]chan Frame{},
		JoinedAt: time.Now(),
	}

	h.mu.Lock()
	if existing, ok := h.agents[agentID]; ok {
		// Replace stale connection
		existing.markClosed()
	}
	h.agents[agentID] = ac
	h.mu.Unlock()
	log.Printf("[agent-hub] %s connected", agentID)

	defer func() {
		h.mu.Lock()
		if h.agents[agentID] == ac {
			delete(h.agents, agentID)
		}
		h.mu.Unlock()
		ac.markClosed()
		log.Printf("[agent-hub] %s disconnected", agentID)
	}()

	// Writer goroutine
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case f, ok := <-ac.out:
				if !ok {
					return
				}
				if err := c.WriteJSON(f); err != nil {
					return
				}
			case <-ping.C:
				if err := c.WriteJSON(Frame{Type: "ping"}); err != nil {
					return
				}
			}
		}
	}()

	// Reader loop
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		switch f.Type {
		case "hello":
			if f.AgentName != "" {
				ac.Name = f.AgentName
			}
			if f.Version != "" {
				ac.Version = f.Version
			}
		case "resp":
			ac.mu.Lock()
			ch, ok := ac.pending[f.ID]
			ac.mu.Unlock()
			if ok {
				select {
				case ch <- f:
				default:
				}
			}
		case "event":
			// Agent forwarded an event from the device. Re-deliver it through
			// the local push handler so it lands in events/storage as usual.
			if h.onEvent != nil {
				h.onEvent(f)
			}
		case "pong", "ping":
			// keepalive
		}
	}

	close(ac.out)
	<-writeDone
}

func (a *AgentConn) markClosed() {
	a.mu.Lock()
	a.closed = true
	for _, ch := range a.pending {
		close(ch)
	}
	a.pending = map[string]chan Frame{}
	a.mu.Unlock()
}

// SetEventSink wires a callback invoked when an agent forwards a device event.
func (h *AgentHub) SetEventSink(fn func(Frame)) {
	h.onEvent = fn
}
