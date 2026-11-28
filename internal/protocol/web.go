package protocol

const (
	WebSessionSchema      = "urn:conn:schema:v1:web-session"
	WebLabStatusSchema    = "urn:conn:schema:v1:web-lab-status"
	WebStreamResyncSchema = "urn:conn:schema:v1:web-stream-resync"
)

// WebSession is the signed-in Human of a Conn Lab browser session. It
// carries the CSRF token, never the session token.
type WebSession struct {
	Human     Human     `json:"human"`
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt Timestamp `json:"expires_at"`
}

func (WebSession) SchemaID() string { return WebSessionSchema }

// WebLabStatus reports, without credentials, whether this server serves
// Conn Lab and which settings browser sign-in still needs.
type WebLabStatus struct {
	LabBuilt        bool     `json:"lab_built"`
	SignInAvailable bool     `json:"sign_in_available"`
	MissingSettings []string `json:"missing_settings"`
	LabURL          string   `json:"lab_url,omitempty"`
}

func (WebLabStatus) SchemaID() string { return WebLabStatusSchema }

// WebStreamResync is the data of the event stream's resync message.
type WebStreamResync struct {
	AsOfEvent int64 `json:"as_of_event"`
}

func (WebStreamResync) SchemaID() string { return WebStreamResyncSchema }
