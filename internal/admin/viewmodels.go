package admin

import "relay-gateway/internal/core"

type StationsPage struct {
	Title    string
	Stations []core.Station
}

type MappingsPage struct {
	Title    string
	Stations []core.Station
	Mappings []core.ModelMapping
}
