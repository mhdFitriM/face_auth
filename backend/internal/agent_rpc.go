package internal

// Frame is the wire envelope sent between cloud and agent over WebSocket.
// Bodies are byte slices — Go's json encoder base64-encodes them automatically.
type Frame struct {
	Type        string            `json:"type"` // hello | req | resp | ping | pong | event
	ID          string            `json:"id,omitempty"`
	AgentID     string            `json:"agentId,omitempty"`
	AgentName   string            `json:"agentName,omitempty"`
	Version     string            `json:"version,omitempty"`

	// Request fields (cloud → agent)
	DeviceID    string            `json:"deviceId,omitempty"`
	BaseURL     string            `json:"baseUrl,omitempty"` // http(s)://ip:port
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Body        []byte            `json:"body,omitempty"`

	// Response fields (agent → cloud)
	Status      int               `json:"status,omitempty"`
	RespHeaders map[string]string `json:"respHeaders,omitempty"`
	RespBody    []byte            `json:"respBody,omitempty"`
	Error       string            `json:"error,omitempty"`

	// Event fields (agent → cloud, when device pushes to agent and we forward to cloud)
	EventPath        string `json:"eventPath,omitempty"`
	EventContentType string `json:"eventContentType,omitempty"`
}
