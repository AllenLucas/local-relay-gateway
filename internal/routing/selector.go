package routing

import (
	"errors"
	"sort"
	"time"

	"relay-gateway/internal/core"
)

// StationScore is the dynamic ranking signal produced by the scoring job for a
// single (protocol, alias, station) tuple. A station with Priority == 0 (auto
// mode) consults this score to decide ordering inside the auto tier.
type StationScore struct {
	Score            float64
	HasEnoughSamples bool
}

// ScoreLookup is the read-only view the selector needs from a scoreboard. It
// is intentionally narrow so the routing package does not depend on the jobs
// package.
type ScoreLookup interface {
	Score(protocol core.Protocol, alias string, stationID int64) (StationScore, bool)
}

type Selector struct {
	now    func() time.Time
	scores ScoreLookup
}

func NewSelector(now func() time.Time) *Selector {
	return NewSelectorWithScores(now, nil)
}

// NewSelectorWithScores wires an optional ScoreLookup. When non-nil, stations
// with Priority == 0 are sorted by their per-(alias, station) score; without
// it (or for stations without sufficient samples) auto stations fall back to
// stable ID-ascending order at the tail of the auto tier.
func NewSelectorWithScores(now func() time.Time, scores ScoreLookup) *Selector {
	if now == nil {
		now = time.Now
	}

	return &Selector{now: now, scores: scores}
}

func (s *Selector) Candidates(
	req core.NormalizedRequest,
	stations []core.Station,
	mappings []core.ModelMapping,
	statuses map[int64]core.StationStatus,
) ([]core.ResolvedTarget, error) {
	mappingByStation := make(map[int64]core.ModelMapping, len(mappings))
	for _, mapping := range mappings {
		if !mapping.Enabled || mapping.Protocol != req.Protocol || mapping.Alias != req.Alias {
			continue
		}
		mappingByStation[mapping.StationID] = mapping
	}

	type ranked struct {
		station core.Station
		mapping core.ModelMapping
		// manual=true means user set Priority>0 and we lock to that order.
		// manual=false (Priority==0) means auto tier, ranked by score.
		manual         bool
		score          float64
		hasScore       bool
	}

	pool := make([]ranked, 0, len(stations))
	for _, station := range stations {
		if !station.Enabled {
			continue
		}
		if !station.SupportsProtocol(req.Protocol) {
			continue
		}
		mapping, ok := mappingByStation[station.ID]
		if !ok {
			continue
		}
		status := statuses[station.ID]
		if status.State != "" && status.State != "healthy" {
			continue
		}

		entry := ranked{station: station, mapping: mapping, manual: station.Priority > 0}
		if !entry.manual && s.scores != nil {
			if score, ok := s.scores.Score(req.Protocol, req.Alias, station.ID); ok && score.HasEnoughSamples {
				entry.score = score.Score
				entry.hasScore = true
			}
		}
		pool = append(pool, entry)
	}

	sort.SliceStable(pool, func(i, j int) bool {
		// Tier 1: manual (Priority>0) outranks auto (Priority==0) unconditionally.
		// This is the contract the user agreed to: explicit user choice wins.
		if pool[i].manual != pool[j].manual {
			return pool[i].manual
		}
		if pool[i].manual {
			if pool[i].station.Priority != pool[j].station.Priority {
				return pool[i].station.Priority > pool[j].station.Priority
			}
			return pool[i].station.ID < pool[j].station.ID
		}
		// Auto tier: stations with enough samples sort by score asc (lower is
		// better — score = p50_latency × (1 + error_rate)); stations without
		// enough samples fall to the tail, ordered by ID asc for determinism.
		if pool[i].hasScore != pool[j].hasScore {
			return pool[i].hasScore
		}
		if pool[i].hasScore {
			if pool[i].score != pool[j].score {
				return pool[i].score < pool[j].score
			}
		}
		return pool[i].station.ID < pool[j].station.ID
	})

	targets := make([]core.ResolvedTarget, 0, len(pool))
	for _, entry := range pool {
		target := core.ResolvedTarget{
			Station: entry.station,
			Mapping: entry.mapping,
		}
		if req.Protocol == core.ProtocolOpenAI {
			target.BaseURL = entry.station.OpenAIBaseURL
			target.APIKey = entry.station.OpenAIAPIKey
		} else {
			target.BaseURL = entry.station.AnthropicBaseURL
			target.APIKey = entry.station.AnthropicAPIKey
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
	status.ConsecutiveFailures++
	status.ConsecutiveRecoveries = 0
	status.LastError = message
	status.LastCheckedAt = now
	if failureThreshold(station) > 0 && status.ConsecutiveFailures >= failureThreshold(station) {
		status.State = "cooldown"
		status.CooldownUntil = now.Add(time.Duration(station.CooldownSeconds) * time.Second)
	}

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

func failureThreshold(station core.Station) int {
	if station.ConsecutiveFailureThreshold <= 0 {
		return 1
	}

	return station.ConsecutiveFailureThreshold
}
