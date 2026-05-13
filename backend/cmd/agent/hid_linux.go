//go:build linux

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Linux input_event struct (24 bytes on 64-bit, 16 on 32-bit; we read fields
// individually so it works on either).
//
//   struct input_event {
//     struct timeval time;
//     __u16 type;
//     __u16 code;
//     __s32 value;
//   };

const (
	evKey       = 0x01
	keyEnter    = 28
	keyKpEnter  = 96
	keyTab      = 15
	keyLShift   = 42
	keyRShift   = 54
)

// US QWERTY scancode → (lower, shifted). Just the keys QR scanners produce.
var scanLower = map[uint16]rune{
	2: '1', 3: '2', 4: '3', 5: '4', 6: '5', 7: '6', 8: '7', 9: '8', 10: '9', 11: '0',
	12: '-', 13: '=',
	16: 'q', 17: 'w', 18: 'e', 19: 'r', 20: 't', 21: 'y', 22: 'u', 23: 'i', 24: 'o', 25: 'p',
	26: '[', 27: ']',
	30: 'a', 31: 's', 32: 'd', 33: 'f', 34: 'g', 35: 'h', 36: 'j', 37: 'k', 38: 'l',
	39: ';', 40: '\'', 41: '`', 43: '\\',
	44: 'z', 45: 'x', 46: 'c', 47: 'v', 48: 'b', 49: 'n', 50: 'm',
	51: ',', 52: '.', 53: '/', 57: ' ',
}
var scanShift = map[uint16]rune{
	2: '!', 3: '@', 4: '#', 5: '$', 6: '%', 7: '^', 8: '&', 9: '*', 10: '(', 11: ')',
	12: '_', 13: '+',
	16: 'Q', 17: 'W', 18: 'E', 19: 'R', 20: 'T', 21: 'Y', 22: 'U', 23: 'I', 24: 'O', 25: 'P',
	26: '{', 27: '}',
	30: 'A', 31: 'S', 32: 'D', 33: 'F', 34: 'G', 35: 'H', 36: 'J', 37: 'K', 38: 'L',
	39: ':', 40: '"', 41: '~', 43: '|',
	44: 'Z', 45: 'X', 46: 'C', 47: 'V', 48: 'B', 49: 'N', 50: 'M',
	51: '<', 52: '>', 53: '?', 57: ' ',
}

// startNativeHID runs an HID reader if QR_DEVICE or QR_DEVICE_AUTO is set.
// On every complete scan (ending with Enter or Tab), it calls onScan(line).
func startNativeHID(onScan func(string)) {
	devPath := getenv("QR_DEVICE", "")
	if devPath == "" && getenv("QR_DEVICE_AUTO", "") != "" {
		if p := autoDiscoverScanner(); p != "" {
			devPath = p
			log.Printf("HID: auto-discovered scanner at %s", devPath)
		}
	}
	if devPath == "" {
		return
	}
	go func() {
		for {
			if err := readScanner(devPath, onScan); err != nil {
				log.Printf("HID: %v — retrying in 3s", err)
				time.Sleep(3 * time.Second)
			}
		}
	}()
}

// autoDiscoverScanner returns the first /dev/input/by-id/usb-*-event-kbd that
// has a name hinting at a scanner/barcode/QR device. Falls back to the second
// kbd if there are multiple (assumes #1 is the user's keyboard).
func autoDiscoverScanner() string {
	matches, _ := filepath.Glob("/dev/input/by-id/usb-*-event-kbd")
	if len(matches) == 0 {
		return ""
	}
	// Prefer names that look scanner-like
	hints := []string{"scanner", "barcode", "qr", "honeywell", "datalogic", "symbol", "zebra", "newland"}
	for _, m := range matches {
		low := strings.ToLower(m)
		for _, h := range hints {
			if strings.Contains(low, h) {
				return m
			}
		}
	}
	// Otherwise take the first non-keyboard candidate (heuristic: skip names with "keyboard")
	for _, m := range matches {
		if !strings.Contains(strings.ToLower(m), "keyboard") {
			return m
		}
	}
	return matches[0]
}

func readScanner(devPath string, onScan func(string)) error {
	f, err := os.Open(devPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", devPath, err)
	}
	defer f.Close()
	log.Printf("HID: listening on %s", devPath)

	// Skip the timeval prefix variably — read the whole struct as raw bytes
	// (16 or 24 bytes). We'll detect the size by peeking the first read.
	var buf [24]byte
	// First read: try 24 bytes
	if _, err := io.ReadFull(f, buf[:24]); err != nil {
		return fmt.Errorf("first read: %w", err)
	}
	// Heuristic: on 64-bit systems we expect 24 bytes per event.
	// On 32-bit systems it's 16 bytes. We'll detect by checking if the
	// "type" field appears at offset 16 (64-bit) or offset 8 (32-bit).
	// All EV_KEY events have type=1, which is uint16 little-endian = 0x01 0x00.
	eventSize := 24
	typeOff := 16
	if !(buf[16] == 1 && buf[17] == 0) && (buf[8] == 1 && buf[9] == 0) {
		eventSize = 16
		typeOff = 8
		log.Printf("HID: 32-bit input_event format detected")
	}

	var line strings.Builder
	var shift bool

	process := func(b []byte) {
		etype := binary.LittleEndian.Uint16(b[typeOff:])
		if etype != evKey {
			return
		}
		code := binary.LittleEndian.Uint16(b[typeOff+2:])
		value := int32(binary.LittleEndian.Uint32(b[typeOff+4:]))

		// Track shift state
		if code == keyLShift || code == keyRShift {
			shift = value != 0
			return
		}
		if value == 0 { // key release — ignore
			return
		}

		// Terminator?
		if code == keyEnter || code == keyKpEnter || code == keyTab {
			s := line.String()
			line.Reset()
			if s != "" {
				onScan(s)
			}
			return
		}

		// Character?
		var r rune
		if shift {
			r = scanShift[code]
		} else {
			r = scanLower[code]
		}
		if r != 0 {
			line.WriteRune(r)
		}
	}

	process(buf[:eventSize])
	for {
		if _, err := io.ReadFull(f, buf[:eventSize]); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		process(buf[:eventSize])
	}
}
