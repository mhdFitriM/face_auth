package main

import (
	"strings"
	"sync"
	"time"
)

// scannerDeviceKey groups the HID collections that belong to one physical USB
// device. Composite scanners commonly expose the decoded POS stream on MI_01
// and keyboard input on MI_00. Windows gives those collections different HID
// paths even though their USB instance portion is the same.
//
// Keep the instance portion in the key so identical scanner models plugged
// into different USB ports remain isolated from one another.
func scannerDeviceKey(hardwareID string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(hardwareID)), "#")
	if len(parts) < 3 {
		return strings.ToLower(strings.TrimSpace(hardwareID))
	}
	descriptor := parts[1]
	tokens := strings.Split(descriptor, "&")
	kept := tokens[:0]
	for _, token := range tokens {
		if (strings.HasPrefix(token, "mi_") && len(token) == 5) ||
			(strings.HasPrefix(token, "col") && len(token) == 5) {
			continue
		}
		kept = append(kept, token)
	}
	return strings.Join(kept, "&") + "#" + parts[2]
}

// scannerModelKey extracts the USB VID/PID shared by sibling HID interfaces.
// It is used only as a guarded fallback when Windows assigns different device
// instance IDs to a scanner's POS and keyboard collections.
func scannerModelKey(hardwareID string) string {
	upper := strings.ToUpper(hardwareID)
	vidAt, pidAt := strings.Index(upper, "VID_"), strings.Index(upper, "PID_")
	if vidAt < 0 || pidAt < 0 || len(upper) < vidAt+8 || len(upper) < pidAt+8 {
		return ""
	}
	return upper[vidAt+4:vidAt+8] + ":" + upper[pidAt+4:pidAt+8]
}

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
