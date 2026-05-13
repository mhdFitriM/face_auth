//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const qrWatcherFileName = "face_auth-qr-watcher.ahk"

// qrWatcherStartupPath returns where the .ahk should be dropped so Windows
// auto-starts it at user login (per-user Startup folder).
func qrWatcherStartupPath() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", errors.New("APPDATA env var is empty")
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", qrWatcherFileName), nil
}

// findAutoHotkeyExe locates an installed AutoHotkey v2 interpreter, returning
// an empty string if AutoHotkey doesn't appear to be installed.
func findAutoHotkeyExe() string {
	candidates := []string{
		`C:\Program Files\AutoHotkey\v2\AutoHotkey64.exe`,
		`C:\Program Files\AutoHotkey\v2\AutoHotkey32.exe`,
		`C:\Program Files\AutoHotkey\AutoHotkey64.exe`,
		`C:\Program Files\AutoHotkey\AutoHotkey.exe`,
		`C:\Program Files (x86)\AutoHotkey\AutoHotkey.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if out, err := exec.Command("where", "AutoHotkey64.exe").Output(); err == nil {
		line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
		if line != "" {
			return line
		}
	}
	if out, err := exec.Command("where", "AutoHotkey.exe").Output(); err == nil {
		line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
		if line != "" {
			return line
		}
	}
	return ""
}

// qrWatcherRunning checks if any AutoHotkey process has the watcher .ahk on
// its command line.
func qrWatcherRunning() bool {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command",
		`Get-CimInstance Win32_Process -Filter "Name LIKE 'AutoHotkey%.exe'" | Select-Object -ExpandProperty CommandLine | Out-String`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(qrWatcherFileName))
}

// QRWatcherStatus is what the dashboard reads via /api/qr-watcher/status.
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
	s := QRWatcherStatus{
		Supported:     true,
		AutoHotkeyURL: autoHotkeyInstallerURL,
	}
	if exe := findAutoHotkeyExe(); exe != "" {
		s.AutoHotkeyInstalled = true
		s.AutoHotkeyPath = exe
	}
	if p, err := qrWatcherStartupPath(); err == nil {
		s.StartupPath = p
		if _, err := os.Stat(p); err == nil {
			s.StartupInstalled = true
		}
	}
	s.Running = qrWatcherRunning()
	if tmp := os.Getenv("TEMP"); tmp != "" {
		s.LogPath = filepath.Join(tmp, "face_auth-qr-watcher.log")
	}
	return s
}

// qrWatcherInstall drops the .ahk into the user's Startup folder so it runs
// on every login, then launches it now via the file association (or directly
// if we found AutoHotkey64.exe).
func qrWatcherInstall() (QRWatcherStatus, error) {
	dst, err := qrWatcherStartupPath()
	if err != nil {
		return qrWatcherStatus(), err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return qrWatcherStatus(), fmt.Errorf("create startup dir: %w", err)
	}
	if err := os.WriteFile(dst, renderedQRWatcherAHK(), 0o644); err != nil {
		return qrWatcherStatus(), fmt.Errorf("write %s: %w", dst, err)
	}
	// Best-effort start. If it fails (e.g. AHK isn't installed), the status
	// the caller reads back will reflect that and the UI prompts to install.
	_ = qrWatcherStart()
	return qrWatcherStatus(), nil
}

// qrWatcherStart launches the script via the AutoHotkey interpreter we
// detected, falling back to the file association if needed.
func qrWatcherStart() error {
	path, err := qrWatcherStartupPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("watcher not installed yet; click 'Install QR watcher' first")
	}
	if qrWatcherRunning() {
		return nil
	}
	if exe := findAutoHotkeyExe(); exe != "" {
		cmd := exec.Command(exe, path)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Start()
	}
	// No AutoHotkey found — try the file association so we at least get a
	// useful Windows error dialog ("How do you want to open this file?")
	// which is the cue to install AutoHotkey.
	cmd := exec.Command("cmd.exe", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("AutoHotkey not installed — download it from %s and try again", autoHotkeyInstallerURL)
	}
	return nil
}

// qrWatcherStop terminates any AutoHotkey process running our script.
func qrWatcherStop() error {
	ps := `Get-CimInstance Win32_Process -Filter "Name LIKE 'AutoHotkey%.exe'" | Where-Object { $_.CommandLine -like '*` + qrWatcherFileName + `*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// qrWatcherUninstall removes the .ahk from Startup and stops the running
// process. Used by the "Remove" button.
func qrWatcherUninstall() error {
	_ = qrWatcherStop()
	p, err := qrWatcherStartupPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
