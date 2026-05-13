package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// AgentConfig is persisted at a per-OS standard location so the agent picks
// it up automatically on subsequent launches.
type AgentConfig struct {
	CloudURL      string `json:"cloud_url"`
	AgentID       string `json:"agent_id"`
	AgentToken    string `json:"agent_token"`
	AgentName     string `json:"agent_name"`
	QRStripPrefix string `json:"qr_strip_prefix,omitempty"`
	QRStripSuffix string `json:"qr_strip_suffix,omitempty"`
	QRListenPort  string `json:"qr_listen_port,omitempty"`
	QRDevice      string `json:"qr_device,omitempty"`
	QRDeviceAuto  bool   `json:"qr_device_auto,omitempty"`
}

func (c *AgentConfig) Valid() bool {
	return c != nil && c.CloudURL != "" && c.AgentID != "" && c.AgentToken != ""
}

// configPath returns where the config JSON lives for the current user/OS.
//
//	Windows: %PROGRAMDATA%\face_auth\agent.json
//	macOS:   ~/Library/Application Support/face_auth/agent.json
//	Linux:   /etc/face_auth/agent.json if writable, else ~/.config/face_auth/agent.json
func configPath() string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("PROGRAMDATA")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "face_auth", "agent.json")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "face_auth", "agent.json")
	default: // linux, freebsd, etc.
		// Prefer /etc if writable (services usually run as root or via systemd)
		etc := "/etc/face_auth/agent.json"
		if err := os.MkdirAll(filepath.Dir(etc), 0o755); err == nil {
			f, err := os.OpenFile(etc, os.O_RDWR|os.O_CREATE, 0o644)
			if err == nil {
				_ = f.Close()
				return etc
			}
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "face_auth", "agent.json")
	}
}

func loadConfig() (*AgentConfig, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c AgentConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveConfig(c *AgentConfig) error {
	if c == nil {
		return errors.New("nil config")
	}
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Restrictive perms — file holds the agent token.
	return os.WriteFile(path, data, 0o600)
}
