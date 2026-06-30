package internal

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
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

	"github.com/google/uuid"
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

	// Optional: route via OTAP push command queue (device dials out to us).
	// When OTAP is true, requests are enqueued for the device to fetch on its
	// next CommandRequest poll and we block for the CommandResult.
	OTAP     bool
	DeviceID string
	store    *Store
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
	// OTAP push takes precedence: the device dials out to us, so no LAN reach
	// (IP/agent) is required. Needs the store to enqueue/await commands.
	if d.Reach == "otap" && hub != nil && hub.store != nil {
		c.OTAP = true
		c.DeviceID = d.DeviceID
		c.store = hub.store
		return c
	}
	if d.AgentID != "" && hub != nil {
		c.AgentID = d.AgentID
		c.Hub = hub
	}
	return c
}

// Do executes an HTTP request, handling Digest 401 challenge transparently.
func (c *ISAPIClient) Do(method, path string, contentType string, bodyBytes []byte) (*http.Response, []byte, error) {
	// If routed via OTAP push queue, delegate completely
	if c.OTAP && c.store != nil {
		return c.doViaOTAP(method, path, contentType, bodyBytes)
	}
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

// doViaOTAP enqueues an ISAPI request into the device's OTAP push command
// queue and blocks until the device fetches it (CommandRequest) and reports
// the result (CommandResult). The device dials out to our push listener, so
// no LAN reachability (direct IP or agent) is required. Mirrors doViaAgent.
func (c *ISAPIClient) doViaOTAP(method, path, contentType string, body []byte) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id := uuid.NewString()
	dataFormat := "json"
	if strings.Contains(strings.ToLower(contentType), "xml") {
		dataFormat = "xml"
	}
	cmd := Command{
		ID:         id,
		DeviceID:   c.DeviceID,
		Method:     method,
		URL:        path,
		DataFormat: dataFormat,
	}
	if len(body) > 0 {
		cmd.BodyBase64 = base64.StdEncoding.EncodeToString(body)
	}
	if err := c.store.EnqueueCommand(ctx, cmd); err != nil {
		return nil, nil, fmt.Errorf("otap enqueue: %w", err)
	}

	respBody, status, err := c.store.AwaitCommandResult(ctx, id, 30*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("otap await: %w", err)
	}
	if status == 0 {
		status = 200
	}

	hdrs := http.Header{}
	hdrs.Set("Content-Type", "application/json")
	synth := &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d", status),
		Header:     hdrs,
		Body:       http.NoBody,
	}
	return synth, []byte(respBody), nil
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
	DeviceName      string `xml:"deviceName" json:"deviceName"`
	DeviceID        string `xml:"deviceID" json:"deviceID"`
	Model           string `xml:"model" json:"model"`
	SerialNumber    string `xml:"serialNumber" json:"serialNumber"`
	FirmwareVersion string `xml:"firmwareVersion" json:"firmwareVersion"`
	MACAddress      string `xml:"macAddress" json:"macAddress"`
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
	EmployeeNo     string
	Name           string
	UserType       string // normal | visitor | blackList
	Gender         string // male | female | unknown
	LongTerm       bool   // if true, validity is ignored
	ValidBegin     string // ISO local, e.g. "2026-05-11T00:00:00"
	ValidEnd       string // ISO local
	DoorRight      string // e.g. "1"
	PlanTemplate   string // e.g. "1"
	LocalUIRight   bool   // true = administrator on the device touchscreen
	CheckUser      bool   // attendance-only mode
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
	EmployeeNo     string           `json:"employeeNo"`
	Name           string           `json:"name"`
	UserType       string           `json:"userType"`
	Gender         string           `json:"gender"`
	Valid          *DeviceUserValid `json:"Valid,omitempty"`
	UserVerifyMode string           `json:"userVerifyMode,omitempty"`
	DoorRight      string           `json:"doorRight,omitempty"`
	RightPlan      []map[string]any `json:"RightPlan,omitempty"`
	LocalUIRight   bool             `json:"localUIRight"`
	CheckUser      bool             `json:"checkUser"`
	Raw            map[string]any   `json:"-"`
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
	// Hikvision's employeeNo follows the same character constraints as FPID
	// (no hyphens / underscores / dots / spaces). Strip aggressively to match
	// what EnrolFace sends — otherwise the face record and user record on the
	// device would have different keys and the device would never match them.
	u.EmployeeNo = sanitizeFPID(u.EmployeeNo)
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
	// Hikvision FPID must be [A-Za-z0-9], max ~32 chars. Hyphens, dots, and
	// underscores trigger "badJsonContent — Exceeding the parameter range
	// limit ... FPID". Defensive scrub so legacy data with hyphenated employee
	// numbers still works.
	fpid = sanitizeFPID(fpid)
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

// ============================================================================
// Phase 2 — Better enrolment: capture-at-device + card / fingerprint write.
//
// "Capture" endpoints ask the reader to acquire a credential live (face shown
// to the camera, card swiped, finger pressed) and return the captured data, so
// enrolment happens at the door instead of uploading a photo. All of these flow
// through Do(), so they inherit OTAP / agent / direct routing automatically.
//
// NOTE: exact ISAPI shapes vary by firmware. These follow the ISAPI Access
// Control spec and return the device's raw response so the caller (and the
// admin UI) can see what came back; verify against the live device firmware.
// ============================================================================

// CaptureFaceData asks the device to capture a live face from its camera and
// return the face record (JSON, may include a faceURL or base64). Pass
// infrared=true to also capture the IR image where supported.
func (c *ISAPIClient) CaptureFaceData(infrared bool) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"CaptureFaceDataCond": map[string]any{
			"captureInfrared": infrared,
			"dataType":        "url",
		},
	})
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/CaptureFaceData?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("capture face: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// CaptureCardInfo asks the device to wait for a card swipe on its reader and
// returns the captured card number.
func (c *ISAPIClient) CaptureCardInfo() (string, error) {
	body, _ := json.Marshal(map[string]any{
		"CaptureCardInfoCond": map[string]any{},
	})
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/CaptureCardInfo?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("capture card: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// CaptureFingerPrint asks the device to capture a fingerprint live on its
// sensor and returns the captured template (base64 in the JSON response).
func (c *ISAPIClient) CaptureFingerPrint(fingerNo int) (string, error) {
	if fingerNo <= 0 {
		fingerNo = 1
	}
	body, _ := json.Marshal(map[string]any{
		"CaptureFingerPrintCond": map[string]any{
			"fingerNo": fingerNo,
		},
	})
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/CaptureFingerPrint?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("capture fingerprint: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// SetCardInfo creates (or, if mode=="modify", updates) a card bound to a user.
// cardType is typically "normalCard". Mirrors UserInfo Record vs Modify.
func (c *ISAPIClient) SetCardInfo(employeeNo, cardNo, cardType, mode string) (string, error) {
	if cardType == "" {
		cardType = "normalCard"
	}
	body, _ := json.Marshal(map[string]any{
		"CardInfo": map[string]any{
			"employeeNo": employeeNo,
			"cardNo":     cardNo,
			"cardType":   cardType,
		},
	})
	method, path := "POST", "/ISAPI/AccessControl/CardInfo/Record?format=json"
	if mode == "modify" {
		method, path = "PUT", "/ISAPI/AccessControl/CardInfo/Modify?format=json"
	}
	resp, respBody, err := c.Do(method, path, "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("set card: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// DeleteCard removes a card by its number.
func (c *ISAPIClient) DeleteCard(cardNo string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"CardInfoDelCond": map[string]any{
			"CardNoList": []map[string]any{{"cardNo": cardNo}},
		},
	})
	resp, respBody, err := c.Do("PUT", "/ISAPI/AccessControl/CardInfo/Delete?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("delete card: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// SetFingerPrint uploads a fingerprint template (base64) for a user/finger.
// enableCardReader binds the print so it works as a credential at the reader.
func (c *ISAPIClient) SetFingerPrint(employeeNo string, fingerPrintID int, fingerData string) (string, error) {
	if fingerPrintID <= 0 {
		fingerPrintID = 1
	}
	body, _ := json.Marshal(map[string]any{
		"FingerPrintCfg": map[string]any{
			"employeeNo":        employeeNo,
			"enableCardReader":  []int{1},
			"fingerPrintID":     fingerPrintID,
			"deleteFingerPrint": false,
			"fingerType":        "normalFP",
			"fingerData":        fingerData,
		},
	})
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/FingerPrintCfg?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("set fingerprint: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// DeleteFingerPrint removes a user's fingerprint(s). fingerPrintID<=0 deletes all.
func (c *ISAPIClient) DeleteFingerPrint(employeeNo string, fingerPrintID int) (string, error) {
	cond := map[string]any{
		"mode":           "byEmployeeNo",
		"EmployeeNoList": []map[string]any{{"employeeNo": employeeNo}},
	}
	if fingerPrintID > 0 {
		cond["fingerPrintID"] = fingerPrintID
	}
	body, _ := json.Marshal(map[string]any{"FingerPrintDelete": cond})
	resp, respBody, err := c.Do("PUT", "/ISAPI/AccessControl/FingerPrintDelete?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("delete fingerprint: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// ============================================================================
// Phase 4 — Live video & intercom (two-way audio).
//
// Live view itself already works via the MJPEG re-multiplexer (/stream.mjpg)
// built on GetSnapshot. These add the intercom control plane: open/close a
// two-way audio channel so an operator can talk to whoever is at the door.
// The audio MEDIA pipeline (streaming G.711 to/from the browser) is a separate
// follow-up; these methods set up and tear down the channel and report support.
// ============================================================================

// Intercom on Hikvision access terminals runs over the VideoIntercom protocol
// (call signaling to a master/indoor station), NOT /ISAPI/System/TwoWayAudio
// (which is for NVR/camera talkback and returns notSupport on these readers).

// GetIntercomCapabilities returns VideoIntercom capabilities (use isSupportCallSignal
// to decide whether intercom is available).
func (c *ISAPIClient) GetIntercomCapabilities() (string, error) {
	resp, body, err := c.Do("GET", "/ISAPI/VideoIntercom/capabilities?format=json", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("videoIntercom capabilities: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// GetCallStatus reports the current call state (idle / ring / onCall / …).
func (c *ISAPIClient) GetCallStatus() (string, error) {
	resp, body, err := c.Do("GET", "/ISAPI/VideoIntercom/callStatus?format=json", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("callStatus: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// SendCallSignal drives the intercom call. cmd is one of:
// request | cancel | answer | reject | bellTimeout | hangUp | deviceOnCall.
func (c *ISAPIClient) SendCallSignal(cmd string) (string, error) {
	if cmd == "" {
		cmd = "request"
	}
	body, _ := json.Marshal(map[string]any{
		"CallSignal": map[string]any{"cmdType": cmd},
	})
	resp, respBody, err := c.Do("PUT", "/ISAPI/VideoIntercom/callSignal?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("callSignal %s: status %d: %s", cmd, resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// ============================================================================
// Phase 3 — Health & access schedules.
//
// GetAcsWorkStatus reports device health (door/lock/tamper/battery/capacity).
// Week plans + plan templates control WHEN a user is allowed through: a week
// plan defines per-weekday time windows, a plan template references a week plan
// (+ optional holiday group), and a user's RightPlan points at a template.
// All route via OTAP / agent / direct through Do(); shapes are firmware-
// dependent, so methods return the device's raw response.
// ============================================================================

// GetAcsWorkStatus returns the access controller's working status JSON.
func (c *ISAPIClient) GetAcsWorkStatus() (string, error) {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/AcsWorkStatus?format=json", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("work status: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// WeekPlanDay is one weekday's allow window in a week plan.
type WeekPlanDay struct {
	Week   string `json:"week"` // "Monday".."Sunday"
	Enable bool   `json:"enable"`
	Begin  string `json:"begin"` // "HH:MM:SS"
	End    string `json:"end"`   // "HH:MM:SS"
}

// SetWeekPlan writes a UserRight week plan (one time segment per weekday) under
// the given plan number. Days not supplied default to a full-day allow window.
func (c *ISAPIClient) SetWeekPlan(planNo int, days []WeekPlanDay) (string, error) {
	if planNo <= 0 {
		planNo = 1
	}
	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	byDay := map[string]WeekPlanDay{}
	for _, d := range days {
		byDay[d.Week] = d
	}
	cfg := make([]map[string]any, 0, len(weekdays))
	for _, wd := range weekdays {
		d, ok := byDay[wd]
		if !ok {
			d = WeekPlanDay{Week: wd, Enable: true, Begin: "00:00:00", End: "23:59:59"}
		}
		if d.Begin == "" {
			d.Begin = "00:00:00"
		}
		if d.End == "" {
			d.End = "23:59:59"
		}
		cfg = append(cfg, map[string]any{
			"week":   wd,
			"id":     1,
			"enable": d.Enable,
			"TimeSegment": map[string]any{
				"beginTime": d.Begin,
				"endTime":   d.End,
			},
		})
	}
	body, _ := json.Marshal(map[string]any{
		"UserRightWeekPlanCfg": map[string]any{
			"enable":      true,
			"WeekPlanCfg": cfg,
		},
	})
	path := fmt.Sprintf("/ISAPI/AccessControl/UserRightWeekPlanCfg/%d?format=json", planNo)
	resp, respBody, err := c.Do("PUT", path, "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("set week plan: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// GetWeekPlan reads a week plan's raw JSON.
func (c *ISAPIClient) GetWeekPlan(planNo int) (string, error) {
	if planNo <= 0 {
		planNo = 1
	}
	path := fmt.Sprintf("/ISAPI/AccessControl/UserRightWeekPlanCfg/%d?format=json", planNo)
	_, body, err := c.Do("GET", path, "", nil)
	return string(body), err
}

// SetPlanTemplate writes a plan template that references a week plan number.
func (c *ISAPIClient) SetPlanTemplate(tplNo int, name string, weekPlanNo int) (string, error) {
	if tplNo <= 0 {
		tplNo = 1
	}
	if weekPlanNo <= 0 {
		weekPlanNo = 1
	}
	if name == "" {
		name = fmt.Sprintf("template%d", tplNo)
	}
	body, _ := json.Marshal(map[string]any{
		"UserRightPlanTemplate": map[string]any{
			"enable":       true,
			"templateName": name,
			"weekPlanNo":   weekPlanNo,
		},
	})
	path := fmt.Sprintf("/ISAPI/AccessControl/UserRightPlanTemplate/%d?format=json", tplNo)
	resp, respBody, err := c.Do("PUT", path, "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("set plan template: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// GetPlanTemplate reads a plan template's raw JSON.
func (c *ISAPIClient) GetPlanTemplate(tplNo int) (string, error) {
	if tplNo <= 0 {
		tplNo = 1
	}
	path := fmt.Sprintf("/ISAPI/AccessControl/UserRightPlanTemplate/%d?format=json", tplNo)
	_, body, err := c.Do("GET", path, "", nil)
	return string(body), err
}

// ============================================================================
// QR-via-camera — let the device's own camera read a user's QR code and
// authenticate (no third-party USB scanner). Whether this is possible depends
// entirely on the device firmware: many terminals report QRCode = notSupport.
// We probe capability first and return the device's raw responses so the admin
// can see exactly what the hardware allows.
// ============================================================================

// GetAccessControlCapabilities returns the raw AccessControl capabilities JSON.
func (c *ISAPIClient) GetAccessControlCapabilities() (string, error) {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/capabilities?format=json", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("capabilities: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// SupportsCameraQR best-effort detects whether the device camera can scan QR
// codes, scanning the capabilities JSON for the various flag names firmware
// uses. Returns (supported, rawCapabilities, error).
func (c *ISAPIClient) SupportsCameraQR() (bool, string, error) {
	raw, err := c.GetAccessControlCapabilities()
	if err != nil {
		return false, raw, err
	}
	low := strings.ToLower(raw)
	// Firmware exposes QR support under several names; treat an explicit
	// "...QRCode...":true / "support" as supported, "notSupport" as not.
	for _, key := range []string{"issupportscanqrcode", "issupportqrcode", "qrcode", "scanqrcode"} {
		if !strings.Contains(low, key) {
			continue
		}
		// Find the snippet after the key and check its value.
		idx := strings.Index(low, key)
		win := low[idx:min(idx+60, len(low))]
		if strings.Contains(win, "true") || strings.Contains(win, "\"support\"") || strings.Contains(win, ">support<") {
			return true, raw, nil
		}
		if strings.Contains(win, "notsupport") || strings.Contains(win, "false") {
			return false, raw, nil
		}
		// Key present but value unclear — report present (caller shows raw).
		return true, raw, nil
	}
	return false, raw, nil
}

// SetQRScanEnabled enables/disables camera QR-code recognition on the device.
// The exact config endpoint is firmware-dependent; this targets the documented
// QR config and returns the device's raw response (a 4xx here usually means the
// firmware has no camera-QR support).
func (c *ISAPIClient) SetQRScanEnabled(enable bool) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"QRCodeCfg": map[string]any{
			"enable": enable,
		},
	})
	resp, respBody, err := c.Do("PUT", "/ISAPI/AccessControl/QRCodeCfg?format=json", "application/json", body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("set QR scan: status %d: %s", resp.StatusCode, string(respBody))
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

// ---------- Wi-Fi (wireless network) ----------
//
// Hikvision face terminals that ship a Wi-Fi module expose it as an extra
// network interface (the wired NIC is id 1, the wireless NIC is usually id 2).
// The relevant ISAPI nodes are:
//
//	GET  /ISAPI/System/Network/interfaces                      — list interfaces
//	GET  /ISAPI/System/Network/interfaces/<id>/wireless        — current Wi-Fi config
//	PUT  /ISAPI/System/Network/interfaces/<id>/wireless        — set Wi-Fi config
//	GET  /ISAPI/System/Network/interfaces/<id>/wireless/accessPointList — scan APs
//
// Schemas vary across firmware, so the read/scan helpers return the raw device
// body and we only construct XML for the write path.

// WifiAccessPoint is one entry from a Wi-Fi scan.
type WifiAccessPoint struct {
	SSID         string `json:"ssid"`
	SignalLevel  int    `json:"signalStrength"`
	SecurityMode string `json:"securityMode"`
	Channel      int    `json:"channel"`
}

// netInterfaceList mirrors /ISAPI/System/Network/interfaces enough to spot the
// wireless NIC's id.
type netInterfaceList struct {
	XMLName    xml.Name `xml:"NetworkInterfaceList"`
	Interfaces []struct {
		ID          string `xml:"id"`
		IfType      string `xml:"ifType"` // e.g. "wireless", "ethernet"
		HasWireless string `xml:"Wireless>enabled"`
	} `xml:"NetworkInterface"`
}

// FindWirelessInterface inspects the device's interface list and returns the id
// of the wireless NIC. Returns ("", error) if the device has no Wi-Fi NIC.
func (c *ISAPIClient) FindWirelessInterface() (string, error) {
	resp, body, err := c.Do("GET", "/ISAPI/System/Network/interfaces", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("interfaces: status %d: %s", resp.StatusCode, string(body))
	}
	var list netInterfaceList
	if err := xml.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("interfaces decode: %w", err)
	}
	for _, ni := range list.Interfaces {
		if strings.EqualFold(ni.IfType, "wireless") || ni.HasWireless != "" {
			if ni.ID != "" {
				return ni.ID, nil
			}
		}
	}
	return "", fmt.Errorf("device reports no wireless interface")
}

// wirelessIfaceID resolves the interface id to use: the caller's value if set,
// otherwise auto-detected, otherwise the common default "2".
func (c *ISAPIClient) wirelessIfaceID(ifID string) string {
	if ifID != "" {
		return ifID
	}
	if detected, err := c.FindWirelessInterface(); err == nil {
		return detected
	}
	return "2"
}

// GetWifi returns the device's current wireless config as the raw device body
// (XML on most firmware). ifID may be empty to auto-detect.
func (c *ISAPIClient) GetWifi(ifID string) (string, error) {
	ifID = c.wirelessIfaceID(ifID)
	path := fmt.Sprintf("/ISAPI/System/Network/interfaces/%s/wireless", ifID)
	resp, body, err := c.Do("GET", path, "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("getWifi: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// ScanWifi asks the device to scan for nearby access points. Not all firmware
// supports this; the raw body is returned alongside any parsed list.
func (c *ISAPIClient) ScanWifi(ifID string) ([]WifiAccessPoint, string, error) {
	ifID = c.wirelessIfaceID(ifID)
	path := fmt.Sprintf("/ISAPI/System/Network/interfaces/%s/wireless/accessPointList", ifID)
	resp, body, err := c.Do("GET", path, "", nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != 200 {
		return nil, string(body), fmt.Errorf("scanWifi: status %d", resp.StatusCode)
	}
	var parsed struct {
		XMLName xml.Name `xml:"AccessPointList"`
		APs     []struct {
			SSID         string `xml:"ssid"`
			SignalLevel  int    `xml:"signalStrength"`
			SecurityMode string `xml:"WirelessSecurity>securityMode"`
			Channel      int    `xml:"channel"`
		} `xml:"AccessPoint"`
	}
	out := []WifiAccessPoint{}
	if xml.Unmarshal(body, &parsed) == nil {
		for _, ap := range parsed.APs {
			out = append(out, WifiAccessPoint{
				SSID:         ap.SSID,
				SignalLevel:  ap.SignalLevel,
				SecurityMode: ap.SecurityMode,
				Channel:      ap.Channel,
			})
		}
	}
	return out, string(body), nil
}

// WifiConfig is the subset of wireless settings we let callers change.
type WifiConfig struct {
	SSID         string // network name to join
	Key          string // pre-shared key / passphrase
	SecurityMode string // disable | WEP | WPA-personal | WPA2-personal (default WPA2-personal)
	Algorithm    string // TKIP | AES | TKIP/AES (default AES)
	Enabled      bool   // turn the radio on (default true)
}

// SetWifi configures the device to join an infrastructure Wi-Fi network. The
// wireless interface keeps DHCP for its IP; only the SSID/security are written.
func (c *ISAPIClient) SetWifi(ifID string, w WifiConfig) (string, error) {
	ifID = c.wirelessIfaceID(ifID)
	if w.SecurityMode == "" {
		w.SecurityMode = "WPA2-personal"
	}
	if w.Algorithm == "" {
		w.Algorithm = "AES"
	}

	var security string
	if strings.EqualFold(w.SecurityMode, "disable") || w.SecurityMode == "" {
		security = `    <WirelessSecurity>
        <securityMode>disable</securityMode>
    </WirelessSecurity>`
	} else if strings.EqualFold(w.SecurityMode, "WEP") {
		security = fmt.Sprintf(`    <WirelessSecurity>
        <securityMode>WEP</securityMode>
        <WEP>
            <authenticationType>open</authenticationType>
            <defaultTransmitKeyIndex>1</defaultTransmitKeyIndex>
            <wepKeyLength>64bit</wepKeyLength>
            <EncryptionKeyList>
                <EncryptionKey>
                    <id>1</id>
                    <WEPEncryptionKey>%s</WEPEncryptionKey>
                </EncryptionKey>
            </EncryptionKeyList>
        </WEP>
    </WirelessSecurity>`, xmlEscape(w.Key))
	} else {
		security = fmt.Sprintf(`    <WirelessSecurity>
        <securityMode>%s</securityMode>
        <WPA>
            <algorithmType>%s</algorithmType>
            <sharedKey>%s</sharedKey>
        </WPA>
    </WirelessSecurity>`, xmlEscape(w.SecurityMode), xmlEscape(w.Algorithm), xmlEscape(w.Key))
	}

	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Wireless version="2.0" xmlns="http://www.hikvision.com/ver20/XMLSchema">
    <enabled>%t</enabled>
    <wirelessNetworkMode>infrastructure</wirelessNetworkMode>
    <ssid>%s</ssid>
    <channel>auto</channel>
%s
</Wireless>`, w.Enabled, xmlEscape(w.SSID), security)

	path := fmt.Sprintf("/ISAPI/System/Network/interfaces/%s/wireless", ifID)
	resp, respBody, err := c.Do("PUT", path, "application/xml", []byte(xmlBody))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return string(respBody), fmt.Errorf("setWifi: status %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// xmlEscape escapes a value for safe interpolation into an XML element body.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
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

// sanitizeFPID strips everything except [A-Za-z0-9] and trims to 32 chars.
// Hikvision rejects FPIDs with hyphens, dots, underscores, spaces, or any
// non-ASCII-alphanumeric characters. The same constraint applies to
// `employeeNo` on the UserInfo endpoint, so this helper is reused there.
func sanitizeFPID(in string) string {
	if in == "" {
		return ""
	}
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			out = append(out, c)
		}
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return string(out)
}
