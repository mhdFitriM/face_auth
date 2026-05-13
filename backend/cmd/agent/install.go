package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// installAsService registers the agent to start automatically with the OS.
// On success it starts the service; the caller is expected to exit shortly
// after so the service-managed copy can take over.
//
// The combined stdout+stderr from the install commands is returned in `out`
// so the setup UI can show it on failure (great for debugging permission
// problems).
func installAsService(exePath string) (out string, err error) {
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		return installWindowsService(abs)
	case "linux":
		return installSystemdService(abs)
	case "darwin":
		return installLaunchdService(abs)
	default:
		return "", fmt.Errorf("automatic install not supported on %s", runtime.GOOS)
	}
}

// ─── Windows ───────────────────────────────────────────────────

func installWindowsService(exePath string) (string, error) {
	const svc = "face_auth-agent"

	// Try a no-op probe to detect whether we have admin already.
	probe, _ := exec.Command("sc.exe", "query", svc).CombinedOutput()
	if !bytes.Contains(probe, []byte("Access is denied")) {
		// Probably elevated (or service is queryable). Try the direct flow first.
		out, err := installWindowsServiceDirect(exePath, svc)
		if err == nil {
			return out, nil
		}
		// If it died on access-denied, fall through to elevated path.
		if !strings.Contains(out, "Access is denied") && !strings.Contains(err.Error(), "Access is denied") {
			return out, err
		}
	}

	// Need UAC. Build a one-shot batch file with every sc command, then run
	// it via PowerShell's `Start-Process -Verb RunAs -Wait` which prompts UAC
	// and waits for completion.
	tmp := os.TempDir()
	batPath := filepath.Join(tmp, "face_auth-install.cmd")
	logPath := filepath.Join(tmp, "face_auth-install.log")
	_ = os.Remove(logPath)

	bat := fmt.Sprintf(`@echo off
setlocal
set LOG=%s
> "%%LOG%%" echo === face_auth-agent install ===
sc stop %s    >> "%%LOG%%" 2>&1
sc delete %s  >> "%%LOG%%" 2>&1
sc create %s binPath= "\"%s\" --service" start= auto DisplayName= "face_auth agent" >> "%%LOG%%" 2>&1
sc description %s "face_auth LAN-side agent" >> "%%LOG%%" 2>&1
sc failure %s reset= 60 actions= restart/5000/restart/10000/restart/30000 >> "%%LOG%%" 2>&1
sc start %s   >> "%%LOG%%" 2>&1
>> "%%LOG%%" echo === done ===
exit /b 0
`, logPath, svc, svc, svc, exePath, svc, svc, svc)
	if err := os.WriteFile(batPath, []byte(bat), 0o644); err != nil {
		return "", fmt.Errorf("write batch: %w", err)
	}

	// PowerShell triggers the UAC prompt and waits. -WindowStyle Hidden keeps the cmd window invisible.
	psScript := fmt.Sprintf(`Start-Process -FilePath %q -Verb RunAs -Wait -WindowStyle Hidden`, batPath)
	psOut, psErr := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript).CombinedOutput()

	logBytes, _ := os.ReadFile(logPath)
	combined := string(logBytes)
	if len(psOut) > 0 {
		combined = string(psOut) + "\n" + combined
	}

	if psErr != nil {
		// User clicked No on UAC, or PowerShell missing
		return combined, fmt.Errorf("elevation cancelled or failed: %w. Try right-clicking the agent .exe → Run as administrator, then click Install again", psErr)
	}
	// Verify service exists
	q, _ := exec.Command("sc.exe", "query", svc).CombinedOutput()
	if !bytes.Contains(q, []byte("RUNNING")) && !bytes.Contains(q, []byte("STARTING")) {
		return combined, fmt.Errorf("install completed but service is not running. See output above")
	}
	return combined, nil
}

func installWindowsServiceDirect(exePath, svc string) (string, error) {
	var buf []byte
	run := func(args ...string) ([]byte, error) {
		b, e := exec.Command("sc.exe", args...).CombinedOutput()
		buf = append(buf, b...)
		buf = append(buf, '\n')
		return b, e
	}
	_, _ = run("stop", svc)
	_, _ = run("delete", svc)
	binPath := fmt.Sprintf(`"%s" --service`, exePath)
	if _, err := run("create", svc, "binPath=", binPath, "start=", "auto", "DisplayName=", "face_auth agent"); err != nil {
		return string(buf), err
	}
	_, _ = run("description", svc, "face_auth LAN-side agent")
	_, _ = run("failure", svc, "reset=", "60", "actions=", "restart/5000/restart/10000/restart/30000")
	if _, err := run("start", svc); err != nil {
		return string(buf), err
	}
	return string(buf), nil
}

// ─── Linux (systemd) ───────────────────────────────────────────

func installSystemdService(exePath string) (string, error) {
	unit := fmt.Sprintf(`[Unit]
Description=face_auth LAN-side agent
After=network.target

[Service]
Type=simple
ExecStart=%s --service
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
`, exePath)

	const path = "/etc/systemd/system/face_auth-agent.service"
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w (run as root or with sudo)", path, err)
	}
	steps := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "face_auth-agent"},
		{"systemctl", "restart", "face_auth-agent"},
	}
	var buf []byte
	for _, s := range steps {
		out, err := exec.Command(s[0], s[1:]...).CombinedOutput()
		buf = append(buf, []byte(fmt.Sprintf("$ %v\n%s\n", s, out))...)
		if err != nil {
			return string(buf), fmt.Errorf("%v: %w", s, err)
		}
	}
	return string(buf), nil
}

// ─── macOS (launchd) ────────────────────────────────────────────

func installLaunchdService(exePath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return "", err
	}
	plistPath := filepath.Join(plistDir, "com.face_auth.agent.plist")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.face_auth.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--service</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/face_auth-agent.log</string>
  <key>StandardErrorPath</key><string>/tmp/face_auth-agent.err.log</string>
</dict>
</plist>
`, exePath)

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return "", err
	}
	var buf []byte
	for _, args := range [][]string{
		{"launchctl", "unload", plistPath},
		{"launchctl", "load", plistPath},
	} {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		buf = append(buf, []byte(fmt.Sprintf("$ %v\n%s\n", args, out))...)
		if err != nil && args[1] == "load" {
			return string(buf), fmt.Errorf("%v: %w", args, err)
		}
	}
	return string(buf), nil
}
