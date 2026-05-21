package admin

import "relay-gateway/internal/core"

type StationsPage struct {
	Title       string
	WriteToken  string
	Stations    []core.Station
	EditStation *core.Station

	ShowOnboarding          bool
	DefaultListenAddr       string
	ExampleOpenAIBaseURL    string
	ExampleAnthropicBaseURL string
	ExampleOpenAIAlias      string
	ExampleAnthropicAlias   string
	ExampleOpenAIModel      string
	ExampleAnthropicModel   string
}

type MappingsPage struct {
	Title           string
	WriteToken      string
	Stations        []core.Station
	StationNameByID map[int64]string
	Mappings        []core.ModelMapping
	EditMapping     *core.ModelMapping
}

type StatusPage struct {
	Title    string
	Stations []core.Station
	Statuses map[int64]core.StationStatus
}

type LogsPage struct {
	Title          string
	RequestLogs    []core.RequestLog
	FailoverEvents []core.FailoverEvent
	UsageByStation []core.UsageRow
	UsageByAlias   []core.UsageRow
}

type SetupPage struct {
	Title           string
	WriteToken      string
	RuntimeFilePath string
	RuntimeWarning  string
}

type RuntimePage struct {
	Title           string
	WriteToken      string
	ListenAddr      string
	OpenAIBaseURL   string
	AnthropicURL    string
	LocalAPIKey     string
	RuntimeError    string
	RuntimeFilePath string
	Saved           bool
}
