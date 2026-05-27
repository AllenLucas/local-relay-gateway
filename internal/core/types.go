package core

import "time"

type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

type Station struct {
	ID                           int64
	Name                         string
	Enabled                      bool
	Priority                     int
	CooldownSeconds              int
	HealthCheckIntervalSeconds   int
	HealthCheckTimeoutSeconds    int
	ConsecutiveFailureThreshold  int
	ConsecutiveRecoveryThreshold int
	OpenAIBaseURL                string
	OpenAIAPIKey                 string
	AnthropicBaseURL             string
	AnthropicAPIKey              string
}

func (s Station) SupportsProtocol(protocol Protocol) bool {
	switch protocol {
	case ProtocolOpenAI:
		return s.OpenAIBaseURL != "" && s.OpenAIAPIKey != ""
	case ProtocolAnthropic:
		return s.AnthropicBaseURL != "" && s.AnthropicAPIKey != ""
	default:
		return false
	}
}

type ModelMapping struct {
	ID            int64
	StationID     int64
	Protocol      Protocol
	Alias         string
	UpstreamModel string
	Enabled       bool
}

type StationStatus struct {
	StationID             int64
	State                 string
	CooldownUntil         time.Time
	ConsecutiveFailures   int
	ConsecutiveRecoveries int
	LastError             string
	LastCheckedAt         time.Time
}

type NormalizedRequest struct {
	Protocol Protocol
	Alias    string
	Stream   bool
	Body     map[string]any
	RawBody  []byte
	Headers  map[string][]string
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type ResolvedTarget struct {
	Station       Station
	Mapping       ModelMapping
	BaseURL       string
	APIKey        string
	RequestedPath string
}

type RequestLog struct {
	ID           int64
	Protocol     Protocol
	Alias        string
	StationName  string
	StatusCode   int
	DurationMS   int64
	WasStream    bool
	DidFailover  bool
	ErrorKind    string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CreatedAt    time.Time
}

type FailoverEvent struct {
	ID              int64
	Protocol        Protocol
	Alias           string
	FromStationName string
	ToStationName   string
	Reason          string
	CreatedAt       time.Time
}

type UpstreamErrorLog struct {
	ID          int64
	Protocol    Protocol
	Alias       string
	StationName string
	StatusCode  int
	ErrorKind   string
	Body        string
	Headers     string
	Truncated   bool
	CreatedAt   time.Time
}

type UsageRow struct {
	StationName  string
	Alias        string
	RequestCount int
	ErrorCount   int
}

type DailyTokenUsageRow struct {
	Day             string
	Protocol        Protocol
	Alias           string
	StationName     string
	RequestCount    int
	CountedRequests int
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
}
