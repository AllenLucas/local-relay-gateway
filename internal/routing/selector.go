package routing

import (
	"errors"
	"sort"
	"time"

	"relay-gateway/internal/core"
)

type Selector struct {
	now func() time.Time
}

func NewSelector(now func() time.Time) *Selector {
	if now == nil {
		now = time.Now
	}

	return &Selector{now: now}
}

func (s *Selector) Candidates(
	req core.NormalizedRequest,
	stations []core.Station,
	mappings []core.ModelMapping,
	statuses map[int64]core.StationStatus,
) ([]core.ResolvedTarget, error) {
	mappingByStation := make(map[int64]core.ModelMapping, len(mappings))
	for _, mapping := range mappings {
		if mapping.Protocol != req.Protocol || mapping.Alias != req.Alias {
			continue
		}
		mappingByStation[mapping.StationID] = mapping
	}

	sort.Slice(stations, func(i int, j int) bool {
		if stations[i].Priority == stations[j].Priority {
			return stations[i].ID < stations[j].ID
		}

		return stations[i].Priority > stations[j].Priority
	})

	now := s.now()
	targets := make([]core.ResolvedTarget, 0, len(stations))
	for _, station := range stations {
		if !station.Enabled {
			continue
		}

		mapping, ok := mappingByStation[station.ID]
		if !ok {
			continue
		}

		status := statuses[station.ID]
		if status.State == "cooldown" && status.CooldownUntil.After(now) {
			continue
		}

		target := core.ResolvedTarget{
			Station: station,
			Mapping: mapping,
		}
		if req.Protocol == core.ProtocolOpenAI {
			target.BaseURL = station.OpenAIBaseURL
			target.APIKey = station.OpenAIAPIKey
		} else {
			target.BaseURL = station.AnthropicBaseURL
			target.APIKey = station.AnthropicAPIKey
		}

		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, errors.New("no eligible upstream station")
	}

	return targets, nil
}

func (s *Selector) RecordFailure(station core.Station, status core.StationStatus, message string) core.StationStatus {
	now := s.now()

	status.StationID = station.ID
	status.State = "cooldown"
	status.CooldownUntil = now.Add(time.Duration(station.CooldownSeconds) * time.Second)
	status.ConsecutiveFailures++
	status.ConsecutiveRecoveries = 0
	status.LastError = message
	status.LastCheckedAt = now

	return status
}

func (s *Selector) RecordSuccess(station core.Station, status core.StationStatus) core.StationStatus {
	now := s.now()

	status.StationID = station.ID
	status.ConsecutiveFailures = 0
	status.ConsecutiveRecoveries++
	status.LastError = ""
	status.LastCheckedAt = now

	if status.ConsecutiveRecoveries >= station.ConsecutiveRecoveryThreshold {
		status.State = "healthy"
		status.CooldownUntil = time.Time{}
	}

	return status
}
