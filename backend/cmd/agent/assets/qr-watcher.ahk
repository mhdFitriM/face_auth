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
    } catch as e {
        Log("  -> ERROR: " e.Message)
        TrayTip("face_auth QR watcher", "Couldn't reach agent at " AGENT_URL "`n" e.Message, 3)
    }
}
