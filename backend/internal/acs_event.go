package internal

// acs_event.go — Access event search against the Hikvision device's local
// event log via POST /ISAPI/AccessControl/AcsEvent?format=json.
//
// Always call assertFeature("accessEventSearch") before using this service.
//
// Note: major/minor codes are decimal in the API payload. The device stores
// them as decimal but docs show hex notation. This file uses decimal throughout.

import (
	"encoding/json"
	"fmt"
	"time"
)

// AcsEventFilter is the app-level filter for searching access events.
// Zero values are omitted from the request (device returns all events).
type AcsEventFilter struct {
	SearchID             string    `json:"searchId,omitempty"`
	StartTime            time.Time `json:"startTime"`
	EndTime              time.Time `json:"endTime"`
	EmployeeNo           string    `json:"employeeNo,omitempty"` // filter by person
	Major                int       `json:"major,omitempty"`      // event major type (0 = all)
	Minor                int       `json:"minor,omitempty"`      // event minor type (0 = all)
	MaxResults           int       `json:"maxResults,omitempty"` // default 30, max varies by firmware
	SearchResultPosition int       `json:"searchResultPosition"` // pagination offset
	PicEnable            bool      `json:"picEnable"`            // include capture pictures
	TimeReverseOrder     bool      `json:"timeReverseOrder"`     // newest first
}

// AcsEventInfo is one raw event record from the device.
type AcsEventInfo struct {
	Major              int     `json:"major"`
	Minor              int     `json:"minor"`
	Time               string  `json:"time"`
	NetUser            string  `json:"netUser,omitempty"`
	RemoteHostAddr     string  `json:"remoteHostAddr,omitempty"`
	CardNo             string  `json:"cardNo,omitempty"`
	CardType           int     `json:"cardType,omitempty"`
	CardReaderNo       int     `json:"cardReaderNo,omitempty"`
	DoorNo             int     `json:"doorNo,omitempty"`
	EmployeeNoString   string  `json:"employeeNoString,omitempty"`
	SerialNo           int64   `json:"serialNo,omitempty"`
	UserType           string  `json:"userType,omitempty"`
	CurrentVerifyMode  string  `json:"currentVerifyMode,omitempty"`
	QRCodeInfo         string  `json:"QRCodeInfo,omitempty"`
	CurrTemperature    float64 `json:"currTemperature,omitempty"`
	IsAbnormalTemp     bool    `json:"isAbnomalTemperature,omitempty"` // note: device typo
	Mask               string  `json:"mask,omitempty"`
	PictureURL         string  `json:"pictureURL,omitempty"`
	Filename           string  `json:"filename,omitempty"`
	Name               string  `json:"name,omitempty"`
	FaceRect           *struct {
		Height float64 `json:"height"`
		Width  float64 `json:"width"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
	} `json:"FaceRect,omitempty"`
}

// AcsEventPage is the paginated result from a single search request.
type AcsEventPage struct {
	SearchID           string         `json:"searchID"`
	ResponseStatusStrg string         `json:"responseStatusStrg"`
	NumOfMatches       int            `json:"numOfMatches"`
	TotalMatches       int            `json:"totalMatches"`
	InfoList           []AcsEventInfo `json:"InfoList"`
}

// NormalizedAccessEvent is the app-level normalized event.
type NormalizedAccessEvent struct {
	Major      int            `json:"major"`
	Minor      int            `json:"minor"`
	MinorHex   string         `json:"minorHex"`
	Message    string         `json:"message"`
	MajorName  string         `json:"majorName"`
	EmployeeNo string         `json:"employeeNo,omitempty"`
	Name       string         `json:"name,omitempty"`
	Time       string         `json:"time"`
	DoorNo     int            `json:"doorNo,omitempty"`
	AuthType   string         `json:"authType,omitempty"`
	PictureURL string         `json:"pictureURL,omitempty"`
	Raw        *AcsEventInfo  `json:"raw,omitempty"`
}

// SearchAcsEventsRaw executes one paginated search request and returns the
// raw AcsEventPage. Caller handles pagination.
func (c *ISAPIClient) SearchAcsEventsRaw(f AcsEventFilter) (*AcsEventPage, error) {
	if f.SearchID == "" {
		f.SearchID = fmt.Sprintf("face-auth-%d", time.Now().UnixMilli())
	}
	if f.MaxResults <= 0 {
		f.MaxResults = 30
	}

	cond := map[string]interface{}{
		"searchID":             f.SearchID,
		"searchResultPosition": f.SearchResultPosition,
		"maxResults":           f.MaxResults,
		"timeReverseOrder":     f.TimeReverseOrder,
		"picEnable":            f.PicEnable,
	}
	if !f.StartTime.IsZero() {
		cond["startTime"] = f.StartTime.Format(time.RFC3339)
	}
	if !f.EndTime.IsZero() {
		cond["endTime"] = f.EndTime.Format(time.RFC3339)
	}
	if f.EmployeeNo != "" {
		cond["employeeNoString"] = f.EmployeeNo
	}
	if f.Major != 0 {
		cond["major"] = f.Major
	}
	if f.Minor != 0 {
		cond["minor"] = f.Minor
	}

	body, _ := json.Marshal(map[string]interface{}{"AcsEventCond": cond})
	resp, respBody, err := c.Do("POST", "/ISAPI/AccessControl/AcsEvent?format=json", "application/json", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("AcsEvent search: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var wrap struct {
		AcsEvent AcsEventPage `json:"AcsEvent"`
	}
	if err := json.Unmarshal(respBody, &wrap); err != nil {
		return nil, fmt.Errorf("AcsEvent decode: %w; body=%s", err, string(respBody))
	}
	return &wrap.AcsEvent, nil
}

// SearchAcsEvents fetches ALL pages matching the filter and returns normalized
// events. Use MaxResults and SearchResultPosition for manual pagination via
// SearchAcsEventsRaw when dealing with large result sets.
func (c *ISAPIClient) SearchAcsEvents(f AcsEventFilter) ([]NormalizedAccessEvent, int, error) {
	if f.MaxResults <= 0 {
		f.MaxResults = 50
	}
	if f.SearchID == "" {
		f.SearchID = fmt.Sprintf("face-auth-%d", time.Now().UnixMilli())
	}

	var out []NormalizedAccessEvent
	totalMatches := 0
	pos := 0

	for {
		f.SearchResultPosition = pos
		page, err := c.SearchAcsEventsRaw(f)
		if err != nil {
			return out, totalMatches, err
		}
		if page.ResponseStatusStrg != "OK" && page.ResponseStatusStrg != "" {
			return out, totalMatches, fmt.Errorf("AcsEvent: device responded with status %q", page.ResponseStatusStrg)
		}
		totalMatches = page.TotalMatches
		for _, ev := range page.InfoList {
			out = append(out, normalizeAcsEvent(ev))
		}
		if page.NumOfMatches < f.MaxResults {
			break
		}
		pos += page.NumOfMatches
		if pos >= page.TotalMatches {
			break
		}
		// Safety: avoid infinite loops on misbehaving firmware
		if len(out) > 5000 {
			break
		}
	}
	return out, totalMatches, nil
}

// normalizeAcsEvent converts a raw AcsEventInfo to a NormalizedAccessEvent.
func normalizeAcsEvent(ev AcsEventInfo) NormalizedAccessEvent {
	decoded := DecodeEvent(ev.Major, ev.Minor)
	return NormalizedAccessEvent{
		Major:      ev.Major,
		Minor:      ev.Minor,
		MinorHex:   decoded.MinorHex,
		Message:    decoded.Message,
		MajorName:  decoded.MajorName,
		EmployeeNo: ev.EmployeeNoString,
		Name:       ev.Name,
		Time:       ev.Time,
		DoorNo:     ev.DoorNo,
		AuthType:   ev.CurrentVerifyMode,
		PictureURL: ev.PictureURL,
		Raw:        &ev,
	}
}

// ParseISAPIStatus parses the common status envelope from a Hikvision JSON response body.
func ParseISAPIStatus(body []byte) ISAPIResponseStatus {
	var s ISAPIResponseStatus
	_ = json.Unmarshal(body, &s)
	return s
}
