;
;   face_auth QR Scanner Watcher  —  Windows / AutoHotkey v2
;
;   Catches keystrokes from a USB QR scanner and forwards each completed scan
;   to the local face_auth-agent. Uses a single persistent InputHook with
;   character + key callbacks, so keystrokes are NEVER missed between scans.
;
;   The "V" flag means input is NOT swallowed — your normal typing still
;   works everywhere, and only the assembled scan strings are forwarded.
;
;   This file is normally installed automatically from the agent's local
;   dashboard at http://127.0.0.1:7780/ (Tools tab → Install QR watcher).
;
#Requires AutoHotkey v2.0
#SingleInstance Force
Persistent

; ─── Configuration ──────────────────────────────────────────────
AGENT_URL  := "http://127.0.0.1:__AGENT_PORT__/scan"
MAX_GAP_MS := 200        ; >200ms between keys = restart buffer (human typing)
MIN_LEN    := 4

; ─── Tray ──────────────────────────────────────────────────────
TraySetIcon("imageres.dll", 110)
global scanCount := 0
UpdateTooltip()

A_TrayMenu.Delete()
A_TrayMenu.Add("face_auth QR watcher", (*) => "")
A_TrayMenu.Add()
A_TrayMenu.Add("Send a test scan", TestScan)
A_TrayMenu.Add("Open log file", OpenLog)
A_TrayMenu.Add("Open dashboard", (*) => Run("http://127.0.0.1:7780/"))
A_TrayMenu.Add()
A_TrayMenu.Add("Exit", (*) => ExitApp())

UpdateTooltip() {
    A_IconTip := "face_auth QR watcher — " scanCount " scans forwarded"
}

LOG_PATH := A_Temp "\face_auth-qr-watcher.log"
FileAppend(FormatTime(A_Now, "yyyy-MM-dd HH:mm:ss") " [start]`n", LOG_PATH)

Log(line) {
    try {
        FileAppend(FormatTime(A_Now, "HH:mm:ss") " " line "`n", LOG_PATH)
    }
}

OpenLog(*) => Run("notepad.exe " LOG_PATH)
TestScan(*)  => Forward("in#TEST_FROM_TRAY")

; ─── HID POS Scanner Buzzer ─────────────────────────────────────
;
;   Newland VID_1EAB / PID_1D22 enumerates two USB interfaces:
;     MI_00 — HID keyboard (keystrokes captured by InputHook below)
;     MI_01 — HID POS Barcode Scanner, UP:008C U:0002 (this is what we open)
;
;   The POS interface accepts HID output reports to trigger the built-in
;   buzzer. Report layout (2 bytes, no report-ID prefix for this device):
;     byte 0 = 0x02  →  one short high beep  (access granted)
;     byte 0 = 0x04  →  two short low beeps  (access denied / error)
;
global hPOSScanner := 0
global posOutLen   := 2

HIDInit() {
    global hPOSScanner, posOutLen
    try {
        Log("HID init: searching for VID_1EAB PID_1D22 POS interface...")
        hPOSScanner := OpenPOSInterface()
        if hPOSScanner {
            prep := 0
            if DllCall("hid\HidD_GetPreparsedData", "Ptr", hPOSScanner, "Ptr*", &prep) {
                caps := Buffer(64, 0)
                DllCall("hid\HidP_GetCaps", "Ptr", prep, "Ptr", caps)
                DllCall("hid\HidD_FreePreparsedData", "Ptr", prep)
                posOutLen := NumGet(caps, 6, "UShort")
                if posOutLen < 1
                    posOutLen := 64
            } else {
                posOutLen := 64
            }
            Log("HID POS scanner opened — output report length: " posOutLen)
        } else {
            Log("HID POS scanner: device not found or not accessible — PC audio fallback")
        }
    } catch as e {
        Log("HID init error: " e.Message " — PC audio fallback")
        hPOSScanner := 0
    }
}

; Find and open the HID POS interface (usage page 0x8C) for VID_1EAB PID_1D22.
; Returns a valid handle on success, 0 on failure.
OpenPOSInterface() {
    guidBuf := Buffer(16, 0)
    DllCall("hid\HidD_GetHidGuid", "Ptr", guidBuf)

    hInfo := DllCall("SetupAPI\SetupDiGetClassDevsW",
        "Ptr", guidBuf, "Ptr", 0, "Ptr", 0, "UInt", 0x12, "Ptr")
    if (hInfo = -1 || !hInfo)
        return 0

    ifData := Buffer(28, 0)
    NumPut("UInt", 28, ifData, 0)
    result := 0

    loop {
        if !DllCall("SetupAPI\SetupDiEnumDeviceInterfaces",
            "Ptr", hInfo, "Ptr", 0, "Ptr", guidBuf, "UInt", A_Index - 1, "Ptr", ifData)
            break

        reqSz := 0
        DllCall("SetupAPI\SetupDiGetDeviceInterfaceDetailW",
            "Ptr", hInfo, "Ptr", ifData, "Ptr", 0, "UInt", 0, "UInt*", &reqSz, "Ptr", 0)
        if !reqSz
            continue

        detail := Buffer(reqSz, 0)
        NumPut("UInt", A_PtrSize = 8 ? 8 : 6, detail, 0)
        if !DllCall("SetupAPI\SetupDiGetDeviceInterfaceDetailW",
            "Ptr", hInfo, "Ptr", ifData, "Ptr", detail, "UInt", reqSz, "Ptr", 0, "Ptr", 0)
            continue

        path := StrGet(detail.Ptr + 4)
        low  := StrLower(path)
        ; Must be our scanner AND the POS interface (mi_01), not the keyboard (mi_00)
        if !InStr(low, "vid_1eab") || !InStr(low, "pid_1d22") || !InStr(low, "mi_01")
            continue

        fh := DllCall("CreateFileW", "Str", path,
            "UInt", 0xC0000000,  ; GENERIC_READ | GENERIC_WRITE
            "UInt", 3,           ; FILE_SHARE_READ | FILE_SHARE_WRITE
            "Ptr",  0,
            "UInt", 3,           ; OPEN_EXISTING
            "UInt", 0,
            "Ptr",  0, "Ptr")
        if (fh = -1 || !fh)
            continue

        ; Double-check: usage page must be 0x8C (POS)
        prep := 0
        if DllCall("hid\HidD_GetPreparsedData", "Ptr", fh, "Ptr*", &prep) {
            caps := Buffer(64, 0)
            DllCall("hid\HidP_GetCaps", "Ptr", prep, "Ptr", caps)
            DllCall("hid\HidD_FreePreparsedData", "Ptr", prep)
            ; HIDP_CAPS: Usage(0,2) UsagePage(2,2)
            if NumGet(caps, 2, "UShort") = 0x8C {
                result := fh
                break
            }
        }
        DllCall("CloseHandle", "Ptr", fh)
    }

    DllCall("SetupAPI\SetupDiDestroyDeviceInfoList", "Ptr", hInfo)
    return result
}

; Low-level helper: send one Newland SSI BEP1 output report.
; Newland SSI command bytes: 7E 01 30 30 30 30 23 42 45 50 31
; Report layout: byte 0 = Report ID (0x04), bytes 1-11 = SSI payload, rest = 0
SendScannerBEP1() {
    global hPOSScanner, posOutLen
    buf := Buffer(posOutLen, 0)
    NumPut("UChar", 0x04,  buf,  0)
    NumPut("UChar", 0x7E,  buf,  1)
    NumPut("UChar", 0x01,  buf,  2)
    NumPut("UChar", 0x30,  buf,  3)
    NumPut("UChar", 0x30,  buf,  4)
    NumPut("UChar", 0x30,  buf,  5)
    NumPut("UChar", 0x30,  buf,  6)
    NumPut("UChar", 0x23,  buf,  7)
    NumPut("UChar", 0x42,  buf,  8)
    NumPut("UChar", 0x45,  buf,  9)
    NumPut("UChar", 0x50,  buf, 10)
    NumPut("UChar", 0x31,  buf, 11)
    return DllCall("hid\HidD_SetOutputReport",
        "Ptr", hPOSScanner, "Ptr", buf, "UInt", posOutLen, "Int")
}

; Trigger the scanner's built-in buzzer for a denied/error result.
; good=true: single beep (not currently used; scanner beeps on its own for success)
; good=false: two beeps 350ms apart
; Returns false if HID write fails (caller should use PC audio fallback).
ScannerBuzz(good) {
    global hPOSScanner
    if !hPOSScanner
        return false
    ok := SendScannerBEP1()
    if !ok {
        Log("HID output report failed (err " DllCall("GetLastError") ") — PC audio fallback")
        hPOSScanner := 0
        return false
    }
    if !good {
        Sleep(350)
        SendScannerBEP1()
    }
    return true
}

; Initialise — run once at startup
HIDInit()

; ─── Persistent keyboard hook ──────────────────────────────────
global buf := "", lastT := 0

ih := InputHook("V I0")          ; V = visible (don't swallow), I0 = no SendLevel filtering
ih.KeyOpt("{All}", "N")          ; notify for every key
ih.OnKeyDown := OnKeyDown
ih.Start()
Log("InputHook started (V I0, notify-all)")

OnKeyDown(IH, VK, SC) {
    global buf, lastT, scanCount, MAX_GAP_MS, MIN_LEN

    now := A_TickCount
    if (now - lastT > MAX_GAP_MS) {
        buf := ""
    }
    lastT := now

    ; Enter (VK 0x0D) or NumpadEnter — flush the buffer
    if (VK = 0x0D) {
        if (StrLen(buf) >= MIN_LEN) {
            text := buf
            buf := ""
            Forward(text)
            scanCount += 1
            UpdateTooltip()
        } else {
            buf := ""
        }
        return
    }

    ; Get the character representation of this key (respects Shift state)
    ch := VKtoChar(VK)
    if (ch != "")
        buf .= ch
}

; Map a virtual-key code to its character (US layout), honouring Shift state.
VKtoChar(VK) {
    static lower := Map(
        0x30, "0", 0x31, "1", 0x32, "2", 0x33, "3", 0x34, "4",
        0x35, "5", 0x36, "6", 0x37, "7", 0x38, "8", 0x39, "9",
        0x41, "a", 0x42, "b", 0x43, "c", 0x44, "d", 0x45, "e",
        0x46, "f", 0x47, "g", 0x48, "h", 0x49, "i", 0x4A, "j",
        0x4B, "k", 0x4C, "l", 0x4D, "m", 0x4E, "n", 0x4F, "o",
        0x50, "p", 0x51, "q", 0x52, "r", 0x53, "s", 0x54, "t",
        0x55, "u", 0x56, "v", 0x57, "w", 0x58, "x", 0x59, "y", 0x5A, "z",
        0xBA, ";", 0xBB, "=", 0xBC, ",", 0xBD, "-", 0xBE, ".",
        0xBF, "/", 0xC0, "``", 0xDB, "[", 0xDC, "\", 0xDD, "]", 0xDE, "'",
        0x20, " ")
    static shifted := Map(
        0x30, ")", 0x31, "!", 0x32, "@", 0x33, "#", 0x34, "$",
        0x35, "%", 0x36, "^", 0x37, "&", 0x38, "*", 0x39, "(",
        0xBA, ":", 0xBB, "+", 0xBC, "<", 0xBD, "_", 0xBE, ">",
        0xBF, "?", 0xC0, "~", 0xDB, "{", 0xDC, "|", 0xDD, "}", 0xDE, '"')

    isShift := GetKeyState("Shift", "P")
    if (VK >= 0x41 && VK <= 0x5A) {
        c := lower.Has(VK) ? lower[VK] : ""
        return isShift ? StrUpper(c) : c
    }
    if (isShift && shifted.Has(VK))
        return shifted[VK]
    return lower.Has(VK) ? lower[VK] : ""
}

Forward(text) {
    Log("forward: " text)
    body := '{"qr":"' StrReplace(text, '"', '\"') '"}'
    try {
        whr := ComObject("WinHttp.WinHttpRequest.5.1")
        whr.Open("POST", AGENT_URL, false)
        whr.SetTimeouts(2000, 2000, 5000, 5000)
        whr.SetRequestHeader("Content-Type", "application/json")
        whr.Send(body)
        Log("  -> " whr.Status " " SubStr(whr.ResponseText, 1, 80))
        if (whr.Status >= 200 && whr.Status < 300) {
            ; Scanner already beeps on successful optical read — no extra sound needed
        } else {
            if !ScannerBuzz(false)
                DeniedBeep()
        }
    } catch as e {
        Log("  -> ERROR: " e.Message)
        if !ScannerBuzz(false)
            ErrorBeep()
        TrayTip("face_auth QR watcher", "Couldn't reach agent at " AGENT_URL "`n" e.Message, 3)
    }
}

; PC audio fallbacks — used only when scanner HID feedback is unavailable
GrantedBeep() {
    SoundBeep(880, 100)
    Sleep(50)
    SoundBeep(1100, 150)
}

DeniedBeep() {
    SoundBeep(420, 180)
    Sleep(90)
    SoundBeep(320, 240)
}

ErrorBeep() {
    SoundBeep(300, 120)
    Sleep(60)
    SoundBeep(300, 120)
    Sleep(60)
    SoundBeep(300, 120)
}
