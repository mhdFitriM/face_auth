//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const macQRWatcherSwift = `import Foundation
import CoreGraphics
import ApplicationServices
import AudioToolbox

let endpoint = URL(string: "http://127.0.0.1:__AGENT_PORT__/scan")!
let logURL = FileManager.default.temporaryDirectory.appendingPathComponent("face_auth-qr-watcher.log")
var buffer = ""
var lastKey = Date.distantPast

func log(_ message: String) {
    let line = "\(ISO8601DateFormatter().string(from: Date())) \(message)\n"
    if let data = line.data(using: .utf8) {
        if !FileManager.default.fileExists(atPath: logURL.path) { FileManager.default.createFile(atPath: logURL.path, contents: nil) }
        if let h = try? FileHandle(forWritingTo: logURL) { try? h.seekToEnd(); try? h.write(contentsOf: data); try? h.close() }
    }
}

func rejectionSound() {
    AudioServicesPlaySystemSound(1053)
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.22) {
        AudioServicesPlaySystemSound(1053)
    }
}

func forward(_ qr: String) {
    guard let body = try? JSONSerialization.data(withJSONObject: ["qr": qr]) else { return }
    var request = URLRequest(url: endpoint)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.httpBody = body
    URLSession.shared.dataTask(with: request) { data, response, error in
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        log(error == nil ? "forward \(qr) -> \(status)" : "forward error: \(error!.localizedDescription)")
        if error != nil || status < 200 || status >= 300 { rejectionSound() }
    }.resume()
}

let callback: CGEventTapCallBack = { _, type, event, _ in
    guard type == .keyDown else { return Unmanaged.passUnretained(event) }
    let now = Date()
    if now.timeIntervalSince(lastKey) > 0.2 { buffer = "" }
    lastKey = now
    let key = event.getIntegerValueField(.keyboardEventKeycode)
    if key == 36 || key == 76 || key == 48 {
        if buffer.count >= 4 { forward(buffer) }
        buffer = ""
        return Unmanaged.passUnretained(event)
    }
    var chars = [UniChar](repeating: 0, count: 8)
    var count = 0
    event.keyboardGetUnicodeString(maxStringLength: 8, actualStringLength: &count, unicodeString: &chars)
    if count > 0 { buffer += String(utf16CodeUnits: chars, count: count) }
    return Unmanaged.passUnretained(event)
}

guard AXIsProcessTrusted() else {
    log("Accessibility permission required for face_auth-qr-watcher")
    let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
    _ = AXIsProcessTrustedWithOptions(options)
    RunLoop.current.run()
    exit(1)
}
let mask = CGEventMask(1 << CGEventType.keyDown.rawValue)
guard let tap = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap, options: .listenOnly, eventsOfInterest: mask, callback: callback, userInfo: nil) else {
    log("Could not create keyboard event tap")
    exit(1)
}
let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0)
CFRunLoopAddSource(CFRunLoopGetCurrent(), source, .commonModes)
CGEvent.tapEnable(tap: tap, enable: true)
log("macOS QR watcher started")
CFRunLoopRun()
`

type QRWatcherStatus struct {
	Supported           bool   `json:"supported"`
	AutoHotkeyInstalled bool   `json:"autohotkey_installed"`
	AutoHotkeyPath      string `json:"autohotkey_path,omitempty"`
	AutoHotkeyURL       string `json:"autohotkey_url"`
	StartupInstalled    bool   `json:"startup_installed"`
	StartupPath         string `json:"startup_path,omitempty"`
	Running             bool   `json:"running"`
	LogPath             string `json:"log_path,omitempty"`
}

func macWatcherPaths() (dir, source, binary, plist string) {
	home, _ := os.UserHomeDir()
	dir = filepath.Join(home, "Library", "Application Support", "face_auth")
	source = filepath.Join(dir, "face_auth-qr-watcher.swift")
	binary = filepath.Join(dir, "face_auth-qr-watcher")
	plist = filepath.Join(home, "Library", "LaunchAgents", "com.faceauth.qr-watcher.plist")
	return
}

func qrWatcherStatus() QRWatcherStatus {
	_, _, binary, plist := macWatcherPaths()
	s := QRWatcherStatus{Supported: true, AutoHotkeyInstalled: true, AutoHotkeyPath: binary, StartupPath: plist, LogPath: filepath.Join(os.TempDir(), "face_auth-qr-watcher.log")}
	if _, err := os.Stat(plist); err == nil {
		s.StartupInstalled = true
	}
	out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/com.faceauth.qr-watcher", os.Getuid())).CombinedOutput()
	s.Running = err == nil && strings.Contains(string(out), "state = running")
	return s
}

func qrWatcherInstall() (QRWatcherStatus, error) {
	dir, source, binary, plist := macWatcherPaths()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return qrWatcherStatus(), err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return qrWatcherStatus(), err
	}
	port := getenv("QR_LISTEN_PORT", "7771")
	script := strings.ReplaceAll(macQRWatcherSwift, "__AGENT_PORT__", port)
	if err := os.WriteFile(source, []byte(script), 0o600); err != nil {
		return qrWatcherStatus(), err
	}
	if out, err := exec.Command("/usr/bin/xcrun", "swiftc", "-O", "-o", binary, source).CombinedOutput(); err != nil {
		return qrWatcherStatus(), fmt.Errorf("compile macOS watcher: %w: %s", err, strings.TrimSpace(string(out)))
	}
	plistBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.faceauth.qr-watcher</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>`, binary, filepath.Join(os.TempDir(), "face_auth-qr-watcher.stdout.log"), filepath.Join(os.TempDir(), "face_auth-qr-watcher.stderr.log"))
	if err := os.WriteFile(plist, []byte(plistBody), 0o644); err != nil {
		return qrWatcherStatus(), err
	}
	_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/com.faceauth.qr-watcher", os.Getuid())).Run()
	if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist).CombinedOutput(); err != nil {
		return qrWatcherStatus(), fmt.Errorf("start macOS watcher: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return qrWatcherStatus(), nil
}

func qrWatcherStart() error {
	_, _, _, plist := macWatcherPaths()
	if _, err := os.Stat(plist); err != nil {
		return fmt.Errorf("watcher not installed")
	}
	cmd := exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/com.faceauth.qr-watcher", os.Getuid()))
	return cmd.Run()
}

func qrWatcherStop() error {
	return exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/com.faceauth.qr-watcher", os.Getuid())).Run()
}

func qrWatcherUninstall() error {
	_ = qrWatcherStop()
	_, source, binary, plist := macWatcherPaths()
	for _, p := range []string{source, binary, plist} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// launchd KeepAlive=true in the plist handles auto-restart on darwin.
func startQRWatchdog(_ context.Context) {}
