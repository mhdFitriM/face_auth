package internal

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// StartDebugListener accepts TCP connections on `port` and dumps the first
// 2KB of each one as a hex + ASCII trace. Used to identify which protocol
// (HTTP / HTTPS-TLS / binary ISUP) a Hik device is actually speaking.
func StartDebugListener(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Printf("[debug] listen %s: %v", port, err)
		return
	}
	log.Printf("[debug] RAW TCP DUMP listening on :%s — point your device's ISUP/OTAP port here to capture bytes", port)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Printf("[debug] accept: %v", err)
				continue
			}
			go handleDebugConn(conn)
		}
	}()
}

func handleDebugConn(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, 2048)
	n, err := io.ReadAtLeast(conn, buf, 1)
	if err != nil && n == 0 {
		return
	}
	got := buf[:n]

	verdict := ClassifyBytes(got)
	log.Printf("[debug] %s: %d bytes — %s", remote, n, verdict)

	port := ""
	if a, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		port = fmt.Sprintf("%d", a.Port)
	}
	BridgeLogPush(ConnectionHit{
		At:         time.Now(),
		Port:       port,
		RemoteAddr: remote,
		Protocol:   verdict,
		Bytes:      n,
		Sample:     HexSample(got, 64),
	})

	// If HTTP, send a minimal 200 so device proceeds to its next step.
	if strings.HasPrefix(string(got), "POST ") || strings.HasPrefix(string(got), "GET ") {
		body := `{"status":200,"code":"0x00000000","errorMsg":"Succeeded."}`
		resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		_, _ = conn.Write([]byte(resp))
	}
}
