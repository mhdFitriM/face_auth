//go:build !linux

package main

import "log"

// startNativeHID is a no-op on non-Linux. The agent's HTTP /scan endpoint
// (POST to localhost:7771/scan) remains available so a small helper script
// can feed it from a USB scanner on Windows/macOS.
func startNativeHID(onScan func(string)) {
	if getenv("QR_DEVICE", "") != "" || getenv("QR_DEVICE_AUTO", "") != "" {
		log.Printf("HID: native scanner reading only supported on Linux on this build; use the /scan HTTP endpoint instead")
	}
}
