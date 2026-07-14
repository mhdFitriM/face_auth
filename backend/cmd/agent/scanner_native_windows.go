//go:build windows

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	rimTypeKeyboard = 1
	ridiDeviceName  = 0x20000007
	ridevInputSink  = 0x00000100
	wmInput         = 0x00FF
	wmDestroy       = 0x0002
	wMKeyDown       = 0x0100
	wMSysKeyDown    = 0x0104
	ridInput        = 0x10000003
	hwndMessage     = ^uintptr(2) // HWND_MESSAGE = -3
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetRawInputDeviceList  = user32.NewProc("GetRawInputDeviceList")
	procGetRawInputDeviceInfoW = user32.NewProc("GetRawInputDeviceInfoW")
	procRegisterRawInput       = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData        = user32.NewProc("GetRawInputData")
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	setupapi                   = windows.NewLazySystemDLL("setupapi.dll")
	hidDLL                     = windows.NewLazySystemDLL("hid.dll")
	procSetupDiGetClassDevsW   = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumInterfaces  = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDetailW      = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroy         = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procHidDGetHidGuid         = hidDLL.NewProc("HidD_GetHidGuid")
	procHidDGetPreparsedData   = hidDLL.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData  = hidDLL.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps            = hidDLL.NewProc("HidP_GetCaps")
)

type rawInputDeviceList struct {
	Handle uintptr
	Type   uint32
}
type rawInputDevice struct {
	UsagePage, Usage uint16
	Flags            uint32
	Target           uintptr
}
type point struct{ X, Y int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type wndClassEx struct {
	Size                               uint32
	Style                              uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

type deviceInterfaceData struct {
	Size      uint32
	ClassGUID windows.GUID
	Flags     uint32
	Reserved  uintptr
}

type hidCaps struct {
	Usage, UsagePage                                                          uint16
	InputReportByteLength                                                     uint16
	OutputReportByteLength                                                    uint16
	FeatureReportByteLength                                                   uint16
	Reserved                                                                  [17]uint16
	NumberLinkCollectionNodes                                                 uint16
	NumberInputButtonCaps, NumberInputValueCaps, NumberInputDataIndices       uint16
	NumberOutputButtonCaps, NumberOutputValueCaps, NumberOutputDataIndices    uint16
	NumberFeatureButtonCaps, NumberFeatureValueCaps, NumberFeatureDataIndices uint16
}

type posDevice struct {
	Scanner     PhysicalScanner
	InputLength int
}

var winScanner = struct {
	sync.RWMutex
	routes     map[string]scannerRoute
	aliases    map[string]scannerRoute
	ambiguous  map[string]bool
	buffers    map[uintptr]*strings.Builder
	shift      map[uintptr]bool
	paths      map[uintptr]string
	pos        map[string]posDevice
	posStarted map[string]bool
	managerCtx context.Context
	rawActive  bool
	posActive  map[string]bool
}{routes: map[string]scannerRoute{}, aliases: map[string]scannerRoute{}, ambiguous: map[string]bool{}, buffers: map[uintptr]*strings.Builder{}, shift: map[uintptr]bool{}, paths: map[uintptr]string{}, pos: map[string]posDevice{}, posStarted: map[string]bool{}, posActive: map[string]bool{}}

var rawInputLoopOnce sync.Once

func rawDevicePath(handle uintptr) string {
	var chars uint32
	procGetRawInputDeviceInfoW.Call(handle, ridiDeviceName, 0, uintptr(unsafe.Pointer(&chars)))
	if chars == 0 {
		return ""
	}
	b := make([]uint16, chars+1)
	r, _, _ := procGetRawInputDeviceInfoW.Call(handle, ridiDeviceName, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&chars)))
	if int32(r) < 0 {
		return ""
	}
	return windows.UTF16ToString(b)
}

func parseScanner(path string) PhysicalScanner {
	upper := strings.ToUpper(path)
	vid, pid := "", ""
	if i := strings.Index(upper, "VID_"); i >= 0 && len(upper) >= i+8 {
		vid = upper[i+4 : i+8]
	}
	if i := strings.Index(upper, "PID_"); i >= 0 && len(upper) >= i+8 {
		pid = upper[i+4 : i+8]
	}
	serial := ""
	parts := strings.Split(strings.TrimPrefix(path, `\\?\`), "#")
	if len(parts) > 2 && !strings.Contains(parts[2], "&") {
		serial = parts[2]
	}
	name := "HID keyboard"
	if vid != "" || pid != "" {
		name = fmt.Sprintf("USB HID %s:%s", vid, pid)
	}
	return PhysicalScanner{HardwareID: path, Name: name, VendorID: vid, ProductID: pid, SerialNumber: serial, DevicePath: path, ReaderMode: "raw_input", Interface: interfaceName(path), Connected: true}
}

func interfaceName(path string) string {
	upper := strings.ToUpper(path)
	if i := strings.Index(upper, "MI_"); i >= 0 && len(upper) >= i+5 {
		return upper[i : i+5]
	}
	return ""
}

func hidInterfacePath(detail []byte) string {
	if len(detail) <= 6 {
		return ""
	}
	u := make([]uint16, 0, (len(detail)-4)/2)
	for i := 4; i+1 < len(detail); i += 2 {
		v := uint16(detail[i]) | uint16(detail[i+1])<<8
		if v == 0 {
			break
		}
		u = append(u, v)
	}
	return windows.UTF16ToString(u)
}

// enumeratePOSScanners discovers HID collections that explicitly identify as
// Barcode Scanner usage 0x02 on the POS usage page 0x8C. Keyboard interfaces
// are deliberately excluded here and remain available through Raw Input.
func enumeratePOSScanners() []posDevice {
	var guid windows.GUID
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	h, _, _ := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(&guid)), 0, 0, 0x12) // PRESENT|DEVICEINTERFACE
	if h == 0 || h == ^uintptr(0) {
		return nil
	}
	defer procSetupDiDestroy.Call(h)
	out := []posDevice{}
	for index := uint32(0); ; index++ {
		data := deviceInterfaceData{Size: uint32(unsafe.Sizeof(deviceInterfaceData{}))}
		r, _, _ := procSetupDiEnumInterfaces.Call(h, 0, uintptr(unsafe.Pointer(&guid)), uintptr(index), uintptr(unsafe.Pointer(&data)))
		if r == 0 {
			break
		}
		var required uint32
		procSetupDiGetDetailW.Call(h, uintptr(unsafe.Pointer(&data)), 0, 0, uintptr(unsafe.Pointer(&required)), 0)
		if required < 8 {
			continue
		}
		detail := make([]byte, required)
		if unsafe.Sizeof(uintptr(0)) == 8 {
			*(*uint32)(unsafe.Pointer(&detail[0])) = 8
		} else {
			*(*uint32)(unsafe.Pointer(&detail[0])) = 6
		}
		r, _, _ = procSetupDiGetDetailW.Call(h, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&detail[0])), uintptr(required), uintptr(unsafe.Pointer(&required)), 0)
		if r == 0 {
			continue
		}
		path := hidInterfacePath(detail)
		if path == "" {
			continue
		}
		p, _ := windows.UTF16PtrFromString(path)
		fh, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		var prep uintptr
		ok, _, _ := procHidDGetPreparsedData.Call(uintptr(fh), uintptr(unsafe.Pointer(&prep)))
		if ok == 0 {
			windows.CloseHandle(fh)
			continue
		}
		var caps hidCaps
		status, _, _ := procHidPGetCaps.Call(prep, uintptr(unsafe.Pointer(&caps)))
		procHidDFreePreparsedData.Call(prep)
		windows.CloseHandle(fh)
		if int32(status) < 0 || caps.UsagePage != 0x8C || caps.Usage != 0x02 || caps.InputReportByteLength == 0 {
			continue
		}
		s := parseScanner(path)
		s.Name = fmt.Sprintf("HID POS scanner %s:%s", s.VendorID, s.ProductID)
		s.ReaderMode, s.UsagePage = "hid_pos", "008C"
		s.InputReportLength, s.OutputReportLength = int(caps.InputReportByteLength), int(caps.OutputReportByteLength)
		out = append(out, posDevice{Scanner: s, InputLength: int(caps.InputReportByteLength)})
	}
	return out
}

func refreshPhysicalScanners() []PhysicalScanner {
	pos := enumeratePOSScanners()
	var count uint32
	procGetRawInputDeviceList.Call(0, uintptr(unsafe.Pointer(&count)), unsafe.Sizeof(rawInputDeviceList{}))
	list := []rawInputDeviceList{}
	if count > 0 {
		list = make([]rawInputDeviceList, count)
		r, _, _ := procGetRawInputDeviceList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(unsafe.Pointer(&count)), unsafe.Sizeof(rawInputDeviceList{}))
		if int32(r) < 0 {
			list = nil
		}
	}
	out := make([]PhysicalScanner, 0, len(pos)+int(count))
	for _, d := range pos {
		out = append(out, d.Scanner)
	}
	winScanner.Lock()
	winScanner.paths = map[uintptr]string{}
	winScanner.pos = map[string]posDevice{}
	for _, d := range pos {
		winScanner.pos[strings.ToLower(d.Scanner.HardwareID)] = d
	}
	for _, d := range list {
		if d.Type != rimTypeKeyboard {
			continue
		}
		path := rawDevicePath(d.Handle)
		if path == "" {
			continue
		}
		winScanner.paths[d.Handle] = path
		out = append(out, parseScanner(path))
	}
	winScanner.Unlock()
	scannerState.Lock()
	scannerState.devices = out
	scannerState.Unlock()
	ensurePOSReaders()
	return out
}

func startNativeScannerManager(ctx context.Context, routes []scannerRoute) error {
	winScanner.Lock()
	winScanner.managerCtx = ctx
	winScanner.routes = map[string]scannerRoute{}
	winScanner.aliases = map[string]scannerRoute{}
	winScanner.ambiguous = map[string]bool{}
	for _, route := range routes {
		exact := strings.ToLower(route.HardwareID)
		winScanner.routes[exact] = route
		key := scannerDeviceKey(route.HardwareID)
		if existing, ok := winScanner.aliases[key]; ok && existing.ScannerID != route.ScannerID {
			delete(winScanner.aliases, key)
			winScanner.ambiguous[key] = true
		} else if !winScanner.ambiguous[key] {
			winScanner.aliases[key] = route
		}
	}
	winScanner.Unlock()
	refreshPhysicalScanners()
	rawInputLoopOnce.Do(func() { go runRawInputLoop(ctx) })
	return nil
}

// scannerRouteLocked first honors an exact interface binding, then accepts a
// sibling HID collection from the same physical USB instance. The alias map is
// deliberately omitted when bindings are ambiguous, preserving fail-closed
// routing for multiple scanners of the same model.
func scannerRouteLocked(hardwareID string) (scannerRoute, bool) {
	if route, ok := winScanner.routes[strings.ToLower(hardwareID)]; ok {
		return route, true
	}
	if route, ok := winScanner.aliases[scannerDeviceKey(hardwareID)]; ok {
		return route, true
	}

	// Some composite HID drivers assign different instance IDs to MI_00 and
	// MI_01, so the physical-device key cannot join them. Fall back to VID/PID
	// only when there is exactly one bound route and exactly one connected Raw
	// Input interface for that model. A second matching scanner makes this
	// ambiguous and therefore remains unbound/fail-closed.
	model := scannerModelKey(hardwareID)
	if model == "" {
		return scannerRoute{}, false
	}
	var candidate scannerRoute
	foundRoute := false
	for boundID, route := range winScanner.routes {
		if scannerModelKey(boundID) != model {
			continue
		}
		if foundRoute && candidate.ScannerID != route.ScannerID {
			return scannerRoute{}, false
		}
		candidate, foundRoute = route, true
	}
	if !foundRoute {
		return scannerRoute{}, false
	}
	matchingRaw := 0
	for _, path := range winScanner.paths {
		if scannerModelKey(path) == model {
			matchingRaw++
		}
	}
	if matchingRaw != 1 {
		return scannerRoute{}, false
	}
	return candidate, true
}

// nativeScannerCaptureStatus reports the capture path that is actually able to
// receive input for a configured route. The dashboard uses this instead of the
// legacy AutoHotkey process state when Windows USB bindings are active.
func nativeScannerCaptureStatus() (active bool, bound int) {
	winScanner.RLock()
	defer winScanner.RUnlock()
	bound = len(winScanner.routes)
	for hardwareID := range winScanner.routes {
		if _, isPOS := winScanner.pos[hardwareID]; isPOS {
			if winScanner.posActive[hardwareID] {
				return true, bound
			}
		}
		if winScanner.rawActive {
			for _, path := range winScanner.paths {
				if _, ok := scannerRouteLocked(path); ok {
					return true, bound
				}
			}
		}
	}
	return false, bound
}

func ensurePOSReaders() {
	winScanner.Lock()
	defer winScanner.Unlock()
	ctx := winScanner.managerCtx
	if ctx == nil {
		return
	}
	for hardwareID, dev := range winScanner.pos {
		if winScanner.posStarted[hardwareID] {
			continue
		}
		winScanner.posStarted[hardwareID] = true
		go readPOSScanner(ctx, hardwareID, dev)
	}
}

func readPOSScanner(ctx context.Context, hardwareID string, dev posDevice) {
	for ctx.Err() == nil {
		p, _ := windows.UTF16PtrFromString(dev.Scanner.DevicePath)
		h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			log.Printf("HID POS %s disconnected: %v", dev.Scanner.Name, err)
			time.Sleep(2 * time.Second)
			continue
		}
		winScanner.Lock()
		winScanner.posActive[hardwareID] = true
		winScanner.Unlock()
		log.Printf("HID POS direct reader active: %s report=%d", dev.Scanner.DevicePath, dev.InputLength)
		buf := make([]byte, dev.InputLength)
		var assembled strings.Builder
		for ctx.Err() == nil {
			var read uint32
			if err := windows.ReadFile(h, buf, &read, nil); err != nil {
				break
			}
			if read == 0 {
				continue
			}
			noteScannerInput(dev.Scanner.HardwareID)
			part, continued, ok := decodePOSReportPart(buf[:read])
			if !ok {
				log.Printf("HID POS undecoded report %s", hex.EncodeToString(buf[:read]))
				assembled.Reset()
				continue
			}
			assembled.WriteString(part)
			if continued {
				continue
			}
			payload := assembled.String()
			assembled.Reset()
			if len(payload) < 4 {
				continue
			}
			winScanner.RLock()
			route, bound := scannerRouteLocked(hardwareID)
			winScanner.RUnlock()
			if !bound {
				continue
			}
			status, body := route.Scan(payload)
			log.Printf("HID POS %s -> cloud: %d %s", route.ScannerID, status, truncate(body, 120))
		}
		windows.CloseHandle(h)
		winScanner.Lock()
		delete(winScanner.posActive, hardwareID)
		winScanner.Unlock()
		if ctx.Err() == nil {
			time.Sleep(time.Second)
		}
	}
}

func runRawInputLoop(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className, _ := syscall.UTF16PtrFromString("FaceAuthRawInputWindow")
	instance, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(rawInputWndProc), Instance: instance, ClassName: className}
	if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Printf("raw input register class: %v", e)
		return
	}
	hwnd, _, e := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, hwndMessage, 0, instance, 0)
	if hwnd == 0 {
		log.Printf("raw input window: %v", e)
		return
	}
	rid := rawInputDevice{UsagePage: 0x01, Usage: 0x06, Flags: ridevInputSink, Target: hwnd}
	if r, _, e := procRegisterRawInput.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid)); r == 0 {
		log.Printf("register raw input: %v", e)
		return
	}
	winScanner.Lock()
	winScanner.rawActive = true
	winScanner.Unlock()
	defer func() {
		winScanner.Lock()
		winScanner.rawActive = false
		winScanner.Unlock()
	}()
	log.Printf("Windows Raw Input scanner capture active (%d bound scanner(s))", len(winScanner.routes))
	go func() { <-ctx.Done(); procPostQuitMessage.Call(0) }()
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func rawInputWndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	if message == wmDestroy {
		procPostQuitMessage.Call(0)
		return 0
	}
	if message != wmInput {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
		return r
	}
	var size uint32
	headerSize := uint32(8 + 2*unsafe.Sizeof(uintptr(0)))
	procGetRawInputData.Call(lparam, ridInput, 0, uintptr(unsafe.Pointer(&size)), uintptr(headerSize))
	if size < headerSize+16 {
		return 0
	}
	b := make([]byte, size)
	r, _, _ := procGetRawInputData.Call(lparam, ridInput, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), uintptr(headerSize))
	if uint32(r) != size {
		return 0
	}
	handle := *(*uintptr)(unsafe.Pointer(&b[8]))
	off := uintptr(headerSize)
	flags := *(*uint16)(unsafe.Pointer(&b[off+2]))
	vkey := *(*uint16)(unsafe.Pointer(&b[off+6]))
	messageType := *(*uint32)(unsafe.Pointer(&b[off+8]))
	winScanner.Lock()
	path := winScanner.paths[handle]
	route, bound := scannerRouteLocked(path)
	if vkey == 0x10 {
		// RI_KEY_BREAK (bit 0) means key-up. Raw Input reports both key-down
		// and key-up, unlike the previous global keyboard hook.
		winScanner.shift[handle] = flags&1 == 0
		winScanner.Unlock()
		return 0
	}
	if messageType != wMKeyDown && messageType != wMSysKeyDown {
		winScanner.Unlock()
		return 0
	}
	if !bound {
		winScanner.Unlock()
		noteScannerInput(path)
		return 0
	}
	buf := winScanner.buffers[handle]
	if buf == nil {
		buf = &strings.Builder{}
		winScanner.buffers[handle] = buf
	}
	if vkey == 0x0D || vkey == 0x09 {
		line := buf.String()
		buf.Reset()
		winScanner.Unlock()
		noteScannerInput(path)
		if line != "" {
			go func() {
				status, body := route.Scan(line)
				log.Printf("Raw Input %s -> cloud: %d %s", route.ScannerID, status, truncate(body, 120))
			}()
		}
		return 0
	}
	if ch := rawVKeyRune(vkey, winScanner.shift[handle]); ch != 0 {
		buf.WriteRune(ch)
	}
	winScanner.Unlock()
	return 0
}

func rawVKeyRune(v uint16, shift bool) rune {
	if v >= 'A' && v <= 'Z' {
		if shift {
			return rune(v)
		}
		return rune(v + 32)
	}
	if v >= '0' && v <= '9' {
		if shift {
			return []rune(")!@#$%^&*(")[v-'0']
		}
		return rune(v)
	}
	switch v {
	case 0x20:
		return ' '
	case 0xBD:
		if shift {
			return '_'
		}
		return '-'
	case 0xBB:
		if shift {
			return '+'
		}
		return '='
	case 0xDB:
		if shift {
			return '{'
		}
		return '['
	case 0xDD:
		if shift {
			return '}'
		}
		return ']'
	case 0xDC:
		if shift {
			return '|'
		}
		return '\\'
	case 0xBA:
		if shift {
			return ':'
		}
		return ';'
	case 0xDE:
		if shift {
			return '"'
		}
		return '\''
	case 0xBC:
		if shift {
			return '<'
		}
		return ','
	case 0xBE:
		if shift {
			return '>'
		}
		return '.'
	case 0xBF:
		if shift {
			return '?'
		}
		return '/'
	}
	return 0
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

var _ = time.Now
