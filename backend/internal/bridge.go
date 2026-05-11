package internal

import (
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// ConnectionHit is one entry in the push-port activity log.
type ConnectionHit struct {
	At         time.Time `json:"at"`
	Port       string    `json:"port"`
	RemoteAddr string    `json:"remoteAddr"`
	Protocol   string    `json:"protocol"` // http_push_sdk / http_other / tls_handshake / hik_binary / unknown
	Path       string    `json:"path,omitempty"`
	Method     string    `json:"method,omitempty"`
	Status     int       `json:"status,omitempty"`
	Bytes      int       `json:"bytes"`
	Sample     string    `json:"sample,omitempty"` // hex preview of first 64 bytes
	DeviceID   string    `json:"deviceId,omitempty"`
}

// BridgeLog keeps the last N hits in memory for live admin display.
type BridgeLog struct {
	mu  sync.RWMutex
	buf []ConnectionHit
	cap int
}

var bridgeLog = &BridgeLog{cap: 200}

func BridgeLogPush(h ConnectionHit) {
	bridgeLog.mu.Lock()
	defer bridgeLog.mu.Unlock()
	bridgeLog.buf = append(bridgeLog.buf, h)
	if len(bridgeLog.buf) > bridgeLog.cap {
		bridgeLog.buf = bridgeLog.buf[len(bridgeLog.buf)-bridgeLog.cap:]
	}
}

func BridgeLogSnapshot() []ConnectionHit {
	bridgeLog.mu.RLock()
	defer bridgeLog.mu.RUnlock()
	out := make([]ConnectionHit, len(bridgeLog.buf))
	copy(out, bridgeLog.buf)
	// reverse so most recent first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func BridgeLogClear() {
	bridgeLog.mu.Lock()
	defer bridgeLog.mu.Unlock()
	bridgeLog.buf = bridgeLog.buf[:0]
}

// ClassifyBytes guesses what the device spoke based on the first bytes.
func ClassifyBytes(b []byte) string {
	if len(b) == 0 {
		return "empty"
	}
	s := string(b)
	if strings.HasPrefix(s, "POST ") || strings.HasPrefix(s, "GET ") || strings.HasPrefix(s, "PUT ") {
		if strings.Contains(s, "/iot/") && strings.Contains(s, "/PUSH/") {
			return "http_push_sdk"
		}
		return "http_other"
	}
	if len(b) >= 3 && b[0] == 0x16 && b[1] == 0x03 {
		return "tls_handshake"
	}
	if b[0] == 0x10 || b[0] == 0x58 {
		return "hik_binary"
	}
	return "unknown"
}

// HexSample returns up to maxBytes hex-encoded for display.
func HexSample(b []byte, maxBytes int) string {
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return hex.EncodeToString(b)
}
