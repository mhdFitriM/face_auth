package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"face_auth/internal"

	"github.com/gorilla/websocket"
)

func main() {
	cloudURL := getenv("CLOUD_URL", "")
	agentID := getenv("AGENT_ID", "")
	agentName := getenv("AGENT_NAME", agentID)
	agentToken := getenv("AGENT_TOKEN", "")

	if cloudURL == "" || agentID == "" || agentToken == "" {
		log.Fatalf("CLOUD_URL, AGENT_ID, and AGENT_TOKEN are required")
	}

	u, err := url.Parse(cloudURL)
	if err != nil {
		log.Fatalf("bad CLOUD_URL: %v", err)
	}
	// Normalise scheme: http -> ws, https -> wss
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	if !strings.HasSuffix(u.Path, "/agent/ws") {
		u.Path = strings.TrimRight(u.Path, "/") + "/agent/ws"
	}
	q := u.Query()
	q.Set("id", agentID)
	q.Set("token", agentToken)
	u.RawQuery = q.Encode()
	dialURL := u.String()

	log.Printf("face_auth-agent %s starting; cloud=%s", agentID, redactToken(dialURL))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		cancel()
	}()

	backoff := time.Second
	for ctx.Err() == nil {
		err := connect(ctx, dialURL, agentID, agentName)
		if ctx.Err() != nil {
			return
		}
		log.Printf("disconnected: %v — retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func connect(ctx context.Context, dialURL, agentID, agentName string) error {
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 10 * time.Second,
	}
	c, _, err := dialer.DialContext(ctx, dialURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	log.Printf("connected as %s", agentID)

	// Send hello
	hello := internal.Frame{Type: "hello", AgentID: agentID, AgentName: agentName, Version: "1.0"}
	if err := c.WriteJSON(hello); err != nil {
		return err
	}

	// Writer pump: serialize writes through a channel
	out := make(chan internal.Frame, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var f internal.Frame
			if err := json.Unmarshal(data, &f); err != nil {
				continue
			}
			switch f.Type {
			case "req":
				go handleRequest(f, out)
			case "ping":
				out <- internal.Frame{Type: "pong"}
			}
		}
	}()

	for {
		select {
		case f := <-out:
			if err := c.WriteJSON(f); err != nil {
				return err
			}
		case <-done:
			return errors.New("read loop ended")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func handleRequest(req internal.Frame, out chan<- internal.Frame) {
	resp := internal.Frame{Type: "resp", ID: req.ID}

	if req.BaseURL == "" || req.Path == "" {
		resp.Error = "missing baseUrl or path"
		out <- resp
		return
	}

	// Parse baseUrl to extract IP/port/scheme
	u, err := url.Parse(req.BaseURL)
	if err != nil {
		resp.Error = "bad baseUrl: " + err.Error()
		out <- resp
		return
	}
	host := u.Hostname()
	port := atoi(u.Port())
	useHTTPS := u.Scheme == "https"

	client := internal.NewISAPIClient(host, port, useHTTPS, req.Username, req.Password)
	httpResp, body, err := client.Do(req.Method, req.Path, req.ContentType, req.Body)
	if err != nil {
		resp.Error = err.Error()
		out <- resp
		return
	}
	resp.Status = httpResp.StatusCode
	resp.RespBody = body
	resp.RespHeaders = map[string]string{}
	for k, v := range httpResp.Header {
		if len(v) > 0 {
			resp.RespHeaders[k] = v[0]
		}
	}
	out <- resp
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return n
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func redactToken(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	if t := q.Get("token"); t != "" {
		q.Set("token", "***")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

