package internal

import (
	"encoding/json"
	"time"
)

// ---------- Multi-tenant + auth ----------

type Tenant struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	PremiseType  string          `json:"premiseType"` // gym | property | office | parking | club | school | hotel | generic
	Timezone     string          `json:"timezone"`
	ContactEmail string          `json:"contactEmail,omitempty"`
	ContactPhone string          `json:"contactPhone,omitempty"`
	Address      string          `json:"address,omitempty"`
	Settings     json.RawMessage `json:"settings,omitempty"`
	Active       bool            `json:"active"`
	CreatedAt    time.Time       `json:"createdAt"`
}

type User struct {
	ID           string     `json:"id"`
	TenantID     *string    `json:"tenantId,omitempty"` // nil = HQ
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"` // hq_admin | tenant_admin | tenant_operator
	Name         string     `json:"name,omitempty"`
	Active       bool       `json:"active"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---------- Membership plans ----------

type Plan struct {
	ID                       string     `json:"id"`
	TenantID                 string     `json:"tenantId"`
	Name                     string     `json:"name"`
	Type                     string     `json:"type"`         // unlimited | credit | rule
	DefaultCredits           int        `json:"defaultCredits"`
	MustExitBeforeReentry    bool       `json:"mustExitBeforeReentry"`
	Active                   bool       `json:"active"`
	CreatedAt                time.Time  `json:"createdAt"`
	Rules                    []PlanRule `json:"rules,omitempty"`
}

type PlanRule struct {
	ID        string `json:"id"`
	PlanID    string `json:"planId"`
	Weekdays  int    `json:"weekdays"`  // bitmask: bit0=Mon .. bit6=Sun (127 = every day)
	StartTime string `json:"startTime"` // HH:MM
	EndTime   string `json:"endTime"`   // HH:MM
	Label     string `json:"label,omitempty"`
}

type PersonPlan struct {
	PersonID         string     `json:"personId"`
	PlanID           string     `json:"planId,omitempty"`
	CreditsRemaining int        `json:"creditsRemaining"`
	Inside           bool       `json:"inside"`
	LastEventAt      *time.Time `json:"lastEventAt,omitempty"`
	AssignedAt       time.Time  `json:"assignedAt"`
}

type AccessLog struct {
	ID         int64     `json:"id"`
	TenantID   string    `json:"tenantId"`
	PersonID   string    `json:"personId,omitempty"`
	EmployeeNo string    `json:"employeeNo,omitempty"`
	DeviceID   string    `json:"deviceId,omitempty"`
	Decision   string    `json:"decision"` // allow | deny | observed
	Reason     string    `json:"reason,omitempty"`
	Direction  string    `json:"direction,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Agent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token,omitempty"` // only shown right after creation
	Online    bool      `json:"online"`
	CreatedAt time.Time `json:"createdAt"`
}

type Device struct {
	DeviceID      string     `json:"deviceID"`
	Name          string     `json:"name"`
	Salt          string     `json:"-"`
	Challenge     string     `json:"-"`
	Iterations    int        `json:"-"`
	Username      string     `json:"username,omitempty"`
	DigestType    string     `json:"digestType,omitempty"`
	IsAuth        bool       `json:"isAuth"`
	IP            string     `json:"ip,omitempty"`
	Port          int        `json:"port,omitempty"`
	UseHTTPS      bool       `json:"useHttps"`
	ISAPIUsername string     `json:"isapiUsername,omitempty"`
	ISAPIPassword string     `json:"-"`
	FDID          string     `json:"fdid,omitempty"`
	FaceLibType   string     `json:"faceLibType,omitempty"`
	Online        bool       `json:"online"`
	LastSeen      *time.Time `json:"lastSeen,omitempty"`
	Model         string     `json:"model,omitempty"`
	Firmware      string     `json:"firmware,omitempty"`
	AgentID       string     `json:"agentId,omitempty"` // if set, ISAPI is routed via this agent
	CreatedAt     time.Time  `json:"createdAt"`

	// Password is a transient field used only when writing; never serialized
	transientPassword string `json:"-"`
}

// Password returns the transient push-SDK password field set via SetPassword.
// (Plain getter so the store layer can reach it without exporting state.)
func (d Device) Password() string { return d.transientPassword }

// SetPassword stores the push-SDK password for use during RegisterDevice.
func (d *Device) SetPassword(p string) { d.transientPassword = p }

type Person struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	EmployeeNo     string          `json:"employeeNo,omitempty"`
	Gender         string          `json:"gender,omitempty"`         // male | female | unknown
	PersonType     string          `json:"personType,omitempty"`     // normal | visitor | blackList
	PersonRole     string          `json:"personRole,omitempty"`     // basic | administrator | operator
	LongTerm       bool            `json:"longTerm"`
	AttendanceOnly bool            `json:"attendanceOnly"`
	DoorRight      string          `json:"doorRight,omitempty"`
	PlanTemplate   string          `json:"planTemplate,omitempty"`
	ValidBegin     *time.Time      `json:"validBegin,omitempty"`
	ValidEnd       *time.Time      `json:"validEnd,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	QRToken        string          `json:"qrToken,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type Face struct {
	ID        string    `json:"id"`
	PersonID  string    `json:"personId"`
	DeviceID  string    `json:"deviceId"`
	ImageKey  string    `json:"imageKey"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type Command struct {
	ID             string     `json:"id"`
	DeviceID       string     `json:"deviceId"`
	Method         string     `json:"method"`
	URL            string     `json:"url"`
	DataFormat     string     `json:"dataFormat"`
	BodyBase64     string     `json:"-"`
	ResponseBody   string     `json:"responseBody,omitempty"`
	ResponseStatus int        `json:"responseStatus,omitempty"`
	Status         string     `json:"status"`
	SentAt         *time.Time `json:"sentAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type Event struct {
	ID         int64           `json:"id"`
	DeviceID   string          `json:"deviceId"`
	EventType  string          `json:"eventType"`
	Raw        json.RawMessage `json:"raw"`
	ImageKey   string          `json:"imageKey,omitempty"`
	ReceivedAt time.Time       `json:"receivedAt"`
}

// ---------- Hik wire models ----------

type hikAuthInfoResponse struct {
	Data hikAuthInfoData `json:"data"`
}

type hikAuthInfoData struct {
	Challenge       string `json:"challenge"`
	Salt            string `json:"salt"`
	Iterations      int    `json:"iterations"`
	IsDataEncrypt   bool   `json:"isDataEncrypt"`
	SecurityVersion []int  `json:"securityVersion"`
	IsAuth          bool   `json:"isAuth"`
}

type hikLoginRequest struct {
	Data struct {
		Username      string `json:"username"`
		LoginPassword string `json:"loginPassword"`
	} `json:"data"`
}

type hikLoginResponse struct {
	Status   int    `json:"status"`
	Code     string `json:"code"`
	ErrorMsg string `json:"errorMsg"`
	Data     struct {
		CommandInterval int `json:"commandInterval"`
		ErrorDelay      int `json:"errorDelay"`
	} `json:"data"`
}

type hikStatusResponse struct {
	Status   int    `json:"status"`
	Code     string `json:"code"`
	ErrorMsg string `json:"errorMsg"`
}

type hikCommandItem struct {
	UUID       string `json:"UUID"`
	URL        string `json:"URL"`
	DataFormat string `json:"dataFormat"`
	Data       string `json:"data"`
}

type hikCommandRequestResponse struct {
	Status      int              `json:"status"`
	Code        string           `json:"code"`
	ErrorMsg    string           `json:"errorMsg"`
	CommandNum  int              `json:"commandNum"`
	CommandList []hikCommandItem `json:"commandList,omitempty"`
}

type hikCommandResultRequest struct {
	CommandList []hikCommandResultItem `json:"commandList"`
}

type hikCommandResultItem struct {
	UUID string `json:"UUID"`
	Data string `json:"data"`
}

type hikCommandResultResponse struct {
	Status           int    `json:"status"`
	Code             string `json:"code"`
	ErrorMsg         string `json:"errorMsg"`
	IsPendingCommand bool   `json:"isPendingCommand"`
}

type hikEventListRequest struct {
	EventList []hikEventItem `json:"eventList"`
}

type hikEventItem struct {
	UUID      string `json:"UUID"`
	EventType string `json:"eventType"`
	Data      string `json:"data"`
}

type hikEventListResponse []hikEventResponseItem

type hikEventResponseItem struct {
	UUID     string `json:"UUID"`
	Status   int    `json:"status"`
	Code     string `json:"code"`
	ErrorMsg string `json:"errorMsg"`
}
