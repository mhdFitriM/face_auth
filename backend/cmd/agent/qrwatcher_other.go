//go:build !windows

package main

import "errors"

// QRWatcherStatus is the same shape the Windows build exposes — kept here so
// dashboard.go is platform-agnostic.
type QRWatcherStatus struct {
	Supported          bool   `json:"supported"`
	AutoHotkeyInstalled bool  `json:"autohotkey_installed"`
	AutoHotkeyPath     string `json:"autohotkey_path,omitempty"`
	AutoHotkeyURL      string `json:"autohotkey_url"`
	StartupInstalled   bool   `json:"startup_installed"`
	StartupPath        string `json:"startup_path,omitempty"`
	Running            bool   `json:"running"`
	LogPath            string `json:"log_path,omitempty"`
}

func qrWatcherStatus() QRWatcherStatus {
	return QRWatcherStatus{Supported: false}
}

func qrWatcherInstall() (QRWatcherStatus, error) {
	return qrWatcherStatus(), errors.New("QR watcher auto-install is Windows-only; on Linux use the native HID device path in Settings")
}

func qrWatcherStart() error {
	return errors.New("QR watcher auto-start is Windows-only")
}

func qrWatcherStop() error {
	return errors.New("QR watcher auto-start is Windows-only")
}

func qrWatcherUninstall() error {
	return errors.New("QR watcher auto-start is Windows-only")
}
