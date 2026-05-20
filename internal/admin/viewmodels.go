package admin

import "relay-gateway/internal/core"

type StationsPage struct {
	Title      string
	WriteToken string
	Stations   []core.Station
}

type MappingsPage struct {
	Title      string
	WriteToken string
	Stations   []core.Station
	Mappings   []core.ModelMapping
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
