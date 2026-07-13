package main

import (
	"strings"
	"sync"
	"time"
)

// decodePOSReportPart implements the USB-IF HID POS Scanned Data Report:
// report ID 2, three symbology bytes, 50 decoded-data bytes, then the
// Decode Data Continued bit. It also accepts a conservative vendor fallback
// containing one unambiguous printable field.
func decodePOSReportPart(report []byte) (payload string, continued, ok bool) {
	if len(report) >= 55 && report[0] == 2 {
		data := report[4:54]
		end := len(data)
		for end > 0 && data[end-1] == 0 {
			end--
		}
		if end == 0 {
			return "", report[54]&1 != 0, false
		}
		for _, b := range data[:end] {
			if b < 0x20 || b > 0x7e {
				return "", report[54]&1 != 0, false
			}
		}
		return string(data[:end]), report[54]&1 != 0, true
	}
	best := ""
	for start := 0; start < len(report); {
		for start < len(report) && (report[start] < 0x20 || report[start] > 0x7e) {
			start++
		}
		end := start
		for end < len(report) && report[end] >= 0x20 && report[end] <= 0x7e {
			end++
		}
		if end-start > len(best) {
			best = string(report[start:end])
		}
		if end == start {
			start++
		} else {
			start = end
		}
	}
	best = strings.TrimSpace(best)
	if len(best) < 4 {
		return "", false, false
	}
	return best, false, true
}

type PhysicalScanner struct {
	HardwareID         string    `json:"hardwareId"`
	Name               string    `json:"name"`
	VendorID           string    `json:"vendorId,omitempty"`
	ProductID          string    `json:"productId,omitempty"`
	SerialNumber       string    `json:"serialNumber,omitempty"`
	DevicePath         string    `json:"devicePath,omitempty"`
	ReaderMode         string    `json:"readerMode,omitempty"` // hid_pos | raw_input
	Interface          string    `json:"interface,omitempty"`
	UsagePage          string    `json:"usagePage,omitempty"`
	InputReportLength  int       `json:"inputReportLength,omitempty"`
	OutputReportLength int       `json:"outputReportLength,omitempty"`
	Connected          bool      `json:"connected"`
	LastInputAt        time.Time `json:"lastInputAt,omitempty"`
}

type scannerRoute struct {
	ScannerID  string
	HardwareID string
	Scan       scanFunc
}

var scannerState = struct {
	sync.RWMutex
	devices    []PhysicalScanner
	lastDevice string
	identifyAt time.Time
}{}

func scannerSnapshot() ([]PhysicalScanner, string) {
	scannerState.RLock()
	defer scannerState.RUnlock()
	out := append([]PhysicalScanner(nil), scannerState.devices...)
	return out, scannerState.lastDevice
}

func beginScannerIdentify() {
	scannerState.Lock()
	scannerState.lastDevice = ""
	scannerState.identifyAt = time.Now()
	scannerState.Unlock()
}

func noteScannerInput(hardwareID string) {
	scannerState.Lock()
	scannerState.lastDevice = hardwareID
	for i := range scannerState.devices {
		if scannerState.devices[i].HardwareID == hardwareID {
			scannerState.devices[i].LastInputAt = time.Now()
		}
	}
	scannerState.Unlock()
}
