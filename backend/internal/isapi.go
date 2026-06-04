package internal

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ISAPIClient calls a Hikvision device's ISAPI endpoints with HTTP Digest auth.
// If AgentID and Hub are set, requests are routed over the agent's WebSocket
// instead of made directly — enabling cloud → LAN-device traversal.
type ISAPIClient struct {
	BaseURL    string
	Username   string
	Password   string
	HTTPClient *http.Client

	nc uint32 // digest nonce counter

	// Optional: route via agent
	AgentID string
	Hub     *AgentHub
}

func NewISAPIClient(ip string, port int, useHTTPS bool, user, pass string) *ISAPIClient {
	scheme := "http"
	if useHTTPS {
		scheme = "https"
	}
	if port == 0 {
		if useHTTPS {
			port = 443
		} else {
			port = 80
		}
	}
	return &ISAPIClient{
		BaseURL:  fmt.Sprintf("%s://%s:%d", scheme, ip, port),
		Username: user,
		Password: pass,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				ResponseHeaderTimeout: 25 * time.Second,
				DisableKeepAlives:     false,
			},
		},
	}
}

// NewISAPIClientForDevice constructs the client and auto-wires an agent route
// if the device has agent_id set and the hub knows that agent.
func NewISAPIClientForDevice(d *Device, hub *AgentHub) *ISAPIClient {
	c := NewISAPIClient(d.IP, d.Port, d.UseHTTPS, d.ISAPIUsername, d.ISAPIPassword)
	if d.AgentID != "" && hub != nil {
		c.AgentID = d.AgentID
		c.Hub = hub
	}
	return c
}

// Do executes an HTTP request, handling Digest 401 challenge transparently.
func (c *ISAPIClient) Do(method, path string, contentType string, bodyBytes []byte) (*http.Response, []byte, error) {
	// If routed via agent, delegate completely
	if c.AgentID != "" && c.Hub != nil {
		return c.doViaAgent(method, path, contentType, bodyBytes)
	}
	url := c.BaseURL + path
	doOnce := func(authHeader string) (*http.Response, []byte, error) {
		var rdr io.Reader
		if bodyBytes != nil {
			rdr = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequest(method, url, rdr)
		if err != nil {
			return nil, nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return resp, respBody, nil
	}

	resp, body, err := doOnce("")
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != 401 {
		return resp, body, nil
	}

	challenge := parseDigestChallenge(resp.Header.Get("WWW-Authenticate"))
	if challenge == nil {
		return resp, body, errors.New("401 without parseable Digest challenge")
	}
	authHeader, err := c.buildDigestAuth(method, path, challenge)
	if err != nil {
		return nil, nil, err
	}
	return doOnce(authHeader)
}

func (c *ISAPIClient) doViaAgent(method, path, contentType string, body []byte) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := c.Hub.Do(ctx, c.AgentID, Frame{
		BaseURL:     c.BaseURL,
		Username:    c.Username,
		Password:    c.Password,
		Method:      method,
		Path:        path,
		ContentType: contentType,
		Body:        body,
	}, 55*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("agent route: %w", err)
	}
	if resp.Error != "" {
		return nil, nil, fmt.Errorf("agent: %s", resp.Error)
	}
	hdrs := http.Header{}
	for k, v := range resp.RespHeaders {
		hdrs.Set(k, v)
	}
	if hdrs.Get("Content-Type") == "" {
		hdrs.Set("Content-Type", "application/json")
	}
	synth := &http.Response{
		StatusCode: resp.Status,
		Status:     fmt.Sprintf("%d", resp.Status),
		Header:     hdrs,
		Body:       http.NoBody,
	}
	return synth, resp.RespBody, nil
}

type digestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Qop       string
	Algorithm string
}

func parseDigestChallenge(h string) *digestChallenge {
	if !strings.HasPrefix(strings.ToLower(h), "digest ") {
		return nil
	}
	d := &digestChallenge{Algorithm: "MD5"}
	rest := h[len("Digest "):]
	for _, p := range splitCommaQuoted(rest) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch strings.ToLower(k) {
		case "realm":
			d.Realm = v
		case "nonce":
			d.Nonce = v
		case "opaque":
			d.Opaque = v
		case "qop":
			d.Qop = v
		case "algorithm":
			d.Algorithm = v
		}
	}
	if d.Nonce == "" {
		return nil
	}
	return d
}

func splitCommaQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case ',':
			if inQuote {
				cur.WriteRune(r)
			} else {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (c *ISAPIClient) buildDigestAuth(method, uri string, d *digestChallenge) (string, error) {
	nc := atomic.AddUint32(&c.nc, 1)
	ncStr := fmt.Sprintf("%08x", nc)

	cnonceBuf := make([]byte, 8)
	_, _ = rand.Read(cnonceBuf)
	cnonce := hex.EncodeToString(cnonceBuf)

	ha1 := md5hex(c.Username + ":" + d.Realm + ":" + c.Password)
	ha2 := md5hex(method + ":" + uri)

	var response string
	qop := d.Qop
	if qop != "" {
		// pick "auth" if listed
		if strings.Contains(qop, "auth") {
			qop = "auth"
		} else {
			qop = strings.Split(qop, ",")[0]
		}
		response = md5hex(ha1 + ":" + d.Nonce + ":" + ncStr + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5hex(ha1 + ":" + d.Nonce + ":" + ha2)
	}

	parts := []string{
		fmt.Sprintf(`username="%s"`, c.Username),
		fmt.Sprintf(`realm="%s"`, d.Realm),
		fmt.Sprintf(`nonce="%s"`, d.Nonce),
		fmt.Sprintf(`uri="%s"`, uri),
		fmt.Sprintf(`algorithm=%s`, d.Algorithm),
		fmt.Sprintf(`response="%s"`, response),
	}
	if qop != "" {
		parts = append(parts,
			fmt.Sprintf(`qop=%s`, qop),
			fmt.Sprintf(`nc=%s`, ncStr),
			fmt.Sprintf(`cnonce="%s"`, cnonce),
		)
	}
	if d.Opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, d.Opaque))
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

// ---------- High-level helpers ----------

type DeviceInfo struct {
	DeviceName     string `xml:"deviceName" json:"deviceName"`
	DeviceID       string `xml:"deviceID" json:"deviceID"`
	Model          string `xml:"model" json:"model"`
	SerialNumber   string `xml:"serialNumber" json:"serialNumber"`
	FirmwareVersion string `xml:"firmwareVersion" json:"firmwareVersion"`
	MACAddress     string `xml:"macAddress" json:"macAddress"`
}

// GetDeviceInfo fetches `/ISAPI/System/deviceInfo`. Hik returns XML by default.
func (c *ISAPIClient) GetDeviceInfo() (*DeviceInfo, error) {
	resp, body, err := c.Do("GET", "/ISAPI/System/deviceInfo", "", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("deviceInfo: status %d: %s", resp.StatusCode, string(body))
	}
	var info struct {
		XMLName xml.Name `xml:"DeviceInfo"`
		DeviceInfo
	}
	if err := xml.Unmarshal(body, &info); err != nil {
		// Try JSON
		var jinfo struct {
			DeviceInfo DeviceInfo `json:"DeviceInfo"`
		}
		if jerr := json.Unmarshal(body, &jinfo); jerr == nil && jinfo.DeviceInfo.Model != "" {
			return &jinfo.DeviceInfo, nil
		}
		return nil, fmt.Errorf("deviceInfo decode: %w; raw=%s", err, string(body))
	}
	return &info.DeviceInfo, nil
}

// HikUserInfo mirrors the fields the device's UserInfo/Record endpoint expects.
type HikUserInfo struct {
	EmployeeNo    string
	Name          string
	UserType      string // normal | visitor | blackList
	Gender        string // male | female | unknown
	LongTerm      bool   // if true, validity is ignored
	ValidBegin    string // ISO local, e.g. "2026-05-11T00:00:00"
	ValidEnd      string // ISO local
	DoorRight     string // e.g. "1"
	PlanTemplate  string // e.g. "1"
	LocalUIRight  bool   // true = administrator on the device touchscreen
	CheckUser     bool   // attendance-only mode
	UserVerifyMode string // e.g. "face" | "cardAndPw" | "" (leave device default)
}

// UpsertUserOnDevice creates the user on the device, or modifies if exists.
// Returns the device's raw response body and any error.
func (c *ISAPIClient) UpsertUserOnDevice(u HikUserInfo) (string, error) {
	body := buildUserInfoBody(u)

	// Try create first
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/UserInfo/Record?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == 200 {
		// Check sub-status — sometimes Hik returns 200 with statusCode=4 (already exists)
		var probe struct {
			StatusCode    int    `json:"statusCode"`
			SubStatusCode string `json:"subStatusCode"`
		}
		if json.Unmarshal(respBody, &probe) == nil && probe.StatusCode == 1 {
			return string(respBody), nil
		}
		// Fall through to Modify if create reported any non-OK substatus
	}

	// Try modify
	resp2, body2, err := c.Do("PUT", "/ISAPI/AccessControl/UserInfo/Modify?format=json", "application/json", body)
	if err != nil {
		return string(respBody), err
	}
	if resp2.StatusCode != 200 {
		return string(body2), fmt.Errorf("modify user: status %d", resp2.StatusCode)
	}
	return string(body2), nil
}

// DeleteUserOnDevice removes a user record by employeeNo.
func (c *ISAPIClient) DeleteUserOnDevice(employeeNo string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"UserInfoDelCond": map[string]any{
			"EmployeeNoList": []map[string]any{{"employeeNo": employeeNo}},
		},
	})
	resp, respBody, err := c.Do("PUT", "/ISAPI/AccessControl/UserInfo/Delete?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("delete user: status %d", resp.StatusCode)
	}
	return string(respBody), nil
}

// DeviceUserRecord is what comes back from /ISAPI/AccessControl/UserInfo/Search.
type DeviceUserRecord struct {
	EmployeeNo string                 `json:"employeeNo"`
	Name       string                 `json:"name"`
	UserType   string                 `json:"userType"`
	Gender     string                 `json:"gender"`
	Valid      *DeviceUserValid       `json:"Valid,omitempty"`
	UserVerifyMode string             `json:"userVerifyMode,omitempty"`
	DoorRight  string                 `json:"doorRight,omitempty"`
	RightPlan  []map[string]any       `json:"RightPlan,omitempty"`
	LocalUIRight bool                 `json:"localUIRight"`
	CheckUser  bool                   `json:"checkUser"`
	Raw        map[string]any         `json:"-"`
}

type DeviceUserValid struct {
	Enable    bool   `json:"enable"`
	BeginTime string `json:"beginTime"`
	EndTime   string `json:"endTime"`
	TimeType  string `json:"timeType"`
}

// ListUsers paginates through every user record stored on the device.
func (c *ISAPIClient) ListUsers() ([]DeviceUserRecord, error) {
	out := []DeviceUserRecord{}
	pos := 0
	page := 50
	for {
		body, _ := json.Marshal(map[string]any{
			"UserInfoSearchCond": map[string]any{
				"searchID":             "face_auth-sync",
				"searchResultPosition": pos,
				"maxResults":           page,
			},
		})
		resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/UserInfo/Search?format=json", "application/json", body)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != 200 {
			return out, fmt.Errorf("list users: status %d: %s", resp.StatusCode, string(respBody))
		}
		var wrap struct {
			UserInfoSearch struct {
				ResponseStatusStrg string             `json:"responseStatusStrg"`
				NumOfMatches       int                `json:"numOfMatches"`
				TotalMatches       int                `json:"totalMatches"`
				UserInfo           []DeviceUserRecord `json:"UserInfo"`
			} `json:"UserInfoSearch"`
		}
		if err := json.Unmarshal(respBody, &wrap); err != nil {
			return out, fmt.Errorf("decode users: %w; body=%s", err, string(respBody))
		}
		out = append(out, wrap.UserInfoSearch.UserInfo...)
		if wrap.UserInfoSearch.NumOfMatches < page {
			break
		}
		pos += wrap.UserInfoSearch.NumOfMatches
		if pos >= wrap.UserInfoSearch.TotalMatches {
			break
		}
	}
	return out, nil
}

// DeviceFaceRecord is what /ISAPI/Intelligent/FDLib/FDSearch returns per face.
type DeviceFaceRecord struct {
	FPID    string `json:"FPID"`
	Name    string `json:"name"`
	FaceURL string `json:"faceURL"`
}

// ListFacesOnDevice returns the face library entries.
func (c *ISAPIClient) ListFacesOnDevice(fdid, faceLibType string) ([]DeviceFaceRecord, error) {
	if fdid == "" {
		fdid = "1"
	}
	if faceLibType == "" {
		faceLibType = "blackFD"
	}
	out := []DeviceFaceRecord{}
	pos := 0
	page := 50
	for {
		body, _ := json.Marshal(map[string]any{
			"searchResultPosition": pos,
			"maxResults":           page,
			"faceLibType":          faceLibType,
			"FDID":                 fdid,
		})
		resp, respBody, err := c.Do("POST", "/ISAPI/Intelligent/FDLib/FDSearch?format=json", "application/json", body)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != 200 {
			return out, fmt.Errorf("list faces: status %d: %s", resp.StatusCode, string(respBody))
		}
		var wrap struct {
			TotalMatches int                `json:"totalMatches"`
			NumOfMatches int                `json:"numOfMatches"`
			MatchList    []DeviceFaceRecord `json:"MatchList"`
		}
		if err := json.Unmarshal(respBody, &wrap); err != nil {
			return out, fmt.Errorf("decode faces: %w", err)
		}
		out = append(out, wrap.MatchList...)
		if wrap.NumOfMatches < page {
			break
		}
		pos += wrap.NumOfMatches
		if pos >= wrap.TotalMatches {
			break
		}
	}
	return out, nil
}

// DeviceCardRecord — one card record from /ISAPI/AccessControl/CardInfo/Search.
type DeviceCardRecord struct {
	EmployeeNo string `json:"employeeNo"`
	CardNo     string `json:"cardNo"`
	CardType   string `json:"cardType"`
}

func (c *ISAPIClient) ListCards() ([]DeviceCardRecord, error) {
	out := []DeviceCardRecord{}
	pos := 0
	page := 50
	for {
		body, _ := json.Marshal(map[string]any{
			"CardInfoSearchCond": map[string]any{
				"searchID":             "face_auth-cards",
				"searchResultPosition": pos,
				"maxResults":           page,
			},
		})
		resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/CardInfo/Search?format=json", "application/json", body)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != 200 {
			// Some firmware doesn't expose CardInfo/Search — return empty instead of erroring out.
			return out, nil
		}
		var wrap struct {
			CardInfoSearch struct {
				NumOfMatches int                `json:"numOfMatches"`
				TotalMatches int                `json:"totalMatches"`
				CardInfo     []DeviceCardRecord `json:"CardInfo"`
			} `json:"CardInfoSearch"`
		}
		if err := json.Unmarshal(respBody, &wrap); err != nil {
			return out, nil
		}
		out = append(out, wrap.CardInfoSearch.CardInfo...)
		if wrap.CardInfoSearch.NumOfMatches < page {
			break
		}
		pos += wrap.CardInfoSearch.NumOfMatches
		if pos >= wrap.CardInfoSearch.TotalMatches {
			break
		}
	}
	return out, nil
}

// OpenDoor remotely opens the given door number.
func (c *ISAPIClient) OpenDoor(doorNo int) (string, error) {
	if doorNo <= 0 {
		doorNo = 1
	}
	xmlBody := `<RemoteControlDoor><cmd>open</cmd></RemoteControlDoor>`
	path := fmt.Sprintf("/ISAPI/AccessControl/RemoteControl/door/%d", doorNo)
	resp, respBody, err := c.Do("PUT", path, "application/xml", []byte(xmlBody))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("open door: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// GetFaceImageFromURL fetches a face JPEG that lives on the device.
// path is the relative URL from FaceURL field (e.g. /LOCALS/pic_xxx.jpg).
func (c *ISAPIClient) GetFaceImageFromURL(path string) ([]byte, string, error) {
	// faceURL sometimes already includes scheme/host — extract just the path.
	if idx := strings.Index(path, "://"); idx > 0 {
		rest := path[idx+3:]
		if slash := strings.Index(rest, "/"); slash > 0 {
			path = rest[slash:]
		}
	}
	resp, body, err := c.Do("GET", path, "", nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("fetch face image: status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	return body, ct, nil
}

// DeleteFaceByEmployeeNo deletes a person record from the device (cascades face).
func (c *ISAPIClient) DeleteUserByEmployeeNo(employeeNo string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"UserInfoDelCond": map[string]any{
			"EmployeeNoList": []map[string]any{{"employeeNo": employeeNo}},
		},
	})
	resp, respBody, err := c.Do("PUT", "/ISAPI/AccessControl/UserInfo/Delete?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("delete user: status %d", resp.StatusCode)
	}
	return string(respBody), nil
}

// GetSnapshot pulls a still JPEG of the current camera frame.
// Tries channel 101 then 1 (different firmware conventions).
func (c *ISAPIClient) GetSnapshot() ([]byte, string, error) {
	for _, path := range []string{
		"/ISAPI/Streaming/channels/101/picture",
		"/ISAPI/Streaming/channels/1/picture",
	} {
		resp, body, err := c.Do("GET", path, "", nil)
		if err != nil {
			continue
		}
		if resp.StatusCode == 200 && len(body) > 100 {
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = "image/jpeg"
			}
			return body, ct, nil
		}
	}
	return nil, "", fmt.Errorf("snapshot unavailable on /ISAPI/Streaming/channels/{101,1}/picture")
}

func buildUserInfoBody(u HikUserInfo) []byte {
	if u.UserType == "" {
		u.UserType = "normal"
	}
	if u.Gender == "" {
		u.Gender = "unknown"
	}
	if u.DoorRight == "" {
		u.DoorRight = "1"
	}
	if u.PlanTemplate == "" {
		u.PlanTemplate = "1"
	}
	if u.ValidBegin == "" {
		u.ValidBegin = "2020-01-01T00:00:00"
	}
	if u.ValidEnd == "" {
		u.ValidEnd = "2037-12-31T23:59:59"
	}

	userInfo := map[string]any{
		"employeeNo":   u.EmployeeNo,
		"name":         u.Name,
		"userType":     u.UserType,
		"gender":       u.Gender,
		"localUIRight": u.LocalUIRight,
		"checkUser":    u.CheckUser,
		"doorRight":    u.DoorRight,
		"RightPlan": []map[string]any{
			{"doorNo": 1, "planTemplateNo": u.PlanTemplate},
		},
		"Valid": map[string]any{
			"enable":    !u.LongTerm,
			"beginTime": u.ValidBegin,
			"endTime":   u.ValidEnd,
			"timeType":  "local",
		},
	}
	if u.UserVerifyMode != "" {
		userInfo["userVerifyMode"] = u.UserVerifyMode
	}
	info := map[string]any{"UserInfo": userInfo}
	out, _ := json.Marshal(info)
	return out
}

// EnrolFace pushes a JPEG to the face library on the device. The device must
// have a user record matching FPID, OR you can enrol with just FPID/name and
// the device will create the user record on the fly (for face-only mode).
func (c *ISAPIClient) EnrolFace(fdid, faceLibType, fpid, name string, jpeg []byte) (string, error) {
	if fdid == "" {
		fdid = "1"
	}
	if faceLibType == "" {
		faceLibType = "blackFD"
	}
	record := map[string]any{
		"faceLibType": faceLibType,
		"FDID":        fdid,
		"FPID":        fpid,
	}
	if name != "" {
		record["name"] = name
	}
	recordJSON, _ := json.Marshal(record)

	body, contentType := buildHikMultipart(map[string]struct {
		ContentType string
		Filename    string
		Data        []byte
	}{
		"FaceDataRecord": {ContentType: "application/json", Data: recordJSON},
		"FaceImage":      {ContentType: "image/jpeg", Filename: "face.jpg", Data: jpeg},
	}, []string{"FaceDataRecord", "FaceImage"})

	path := "/ISAPI/Intelligent/FDLib/FaceDataRecord?format=json"
	resp, respBody, err := c.Do("POST", path, contentType, body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("enrol: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// DeleteFace removes a face by FPID.
func (c *ISAPIClient) DeleteFace(fdid, faceLibType, fpid string) (string, error) {
	if fdid == "" {
		fdid = "1"
	}
	if faceLibType == "" {
		faceLibType = "blackFD"
	}
	body, _ := json.Marshal(map[string]any{
		"FPID": []map[string]any{{"value": fpid}},
	})
	path := fmt.Sprintf("/ISAPI/Intelligent/FDLib/FDSearch/Delete?format=json&FDID=%s&faceLibType=%s", fdid, faceLibType)
	resp, respBody, err := c.Do("PUT", path, "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("delete: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// SearchFaces lists faces in the library.
func (c *ISAPIClient) SearchFaces(fdid, faceLibType string, maxResults int) (string, error) {
	if fdid == "" {
		fdid = "1"
	}
	if faceLibType == "" {
		faceLibType = "blackFD"
	}
	if maxResults <= 0 {
		maxResults = 100
	}
	body, _ := json.Marshal(map[string]any{
		"searchResultPosition": 0,
		"maxResults":           maxResults,
		"faceLibType":          faceLibType,
		"FDID":                 fdid,
	})
	_, respBody, err := c.Do("POST", "/ISAPI/Intelligent/FDLib/FDSearch?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}

// SetAlarmHost configures the device to push events to our HTTP listener.
// callbackURL is the path on our server (e.g., "/hik-event").
func (c *ISAPIClient) SetAlarmHost(hostIP string, hostPort int, callbackPath string, slot int) (string, error) {
	if slot <= 0 {
		slot = 1
	}
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<HttpHostNotification>
    <id>%d</id>
    <url>%s</url>
    <protocolType>HTTP</protocolType>
    <parameterFormatType>JSON</parameterFormatType>
    <addressingFormatType>ipaddress</addressingFormatType>
    <ipAddress>%s</ipAddress>
    <portNo>%d</portNo>
    <httpAuthenticationMethod>none</httpAuthenticationMethod>
    <uploadImagesDataType>URL</uploadImagesDataType>
</HttpHostNotification>`, slot, callbackPath, hostIP, hostPort)
	path := fmt.Sprintf("/ISAPI/Event/notification/httpHosts/%d", slot)
	resp, respBody, err := c.Do("PUT", path, "application/xml", []byte(xmlBody))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("setAlarmHost: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// ---------- Helpers ----------

// buildHikMultipart builds a multipart/form-data body in the order given.
// (Hik is picky about part ordering — FaceDataRecord MUST come before FaceImage.)
func buildHikMultipart(parts map[string]struct {
	ContentType string
	Filename    string
	Data        []byte
}, order []string) ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range order {
		p := parts[name]
		hdr := textproto.MIMEHeader{}
		if p.Filename != "" {
			hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, name, p.Filename))
		} else {
			hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q`, name))
		}
		hdr.Set("Content-Type", p.ContentType)
		hdr.Set("Content-Length", strconv.Itoa(len(p.Data)))
		w, err := mw.CreatePart(hdr)
		if err != nil {
			continue
		}
		_, _ = w.Write(p.Data)
	}
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}
