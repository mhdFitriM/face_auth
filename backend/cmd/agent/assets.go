package main

import (
	_ "embed"
	"strings"
)

//go:embed assets/qr-watcher.ahk
var qrWatcherAHK string

// renderedQRWatcherAHK returns the embedded AHK script with the agent's
// current QR-listen port substituted in so the scanner always POSTs to the
// right place even if the user customised the port.
func renderedQRWatcherAHK() []byte {
	port := "7771"
	if cfg, _ := loadConfig(); cfg != nil && cfg.QRListenPort != "" {
		port = cfg.QRListenPort
	}
	return []byte(strings.ReplaceAll(qrWatcherAHK, "__AGENT_PORT__", port))
}

// AutoHotkey v2 stable download URL — the QR-watcher script is v2 syntax.
const autoHotkeyInstallerURL = "https://www.autohotkey.com/download/ahk-v2.exe"
