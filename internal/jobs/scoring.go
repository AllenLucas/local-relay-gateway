package jobs

import (
	"context"
	"sort"
	"sync"
	"time"

	"relay-gateway/internal/core"
	"relay-gateway/internal/routing"
)

// Scoring constants are deliberately exposed as variables (not consts) so tests
// can override the window/tick to drive deterministic recomputation. Defaults
// match the user-approved plan: 15-minute window, 5-minute tick, 10-sample
// floor. Adjusting these in code (not at runtime) is a conscious design choice;
// adding runtime knobs would require exposing them in the admin UI and
// validating safe ranges — out of scope here.
var (
	ScoringWindow    = 15 * time.Minute
	ScoringInterval  = 5 * time.Minute
	ScoringMinSample = 10
)

type scoringStore interface {
	ListStations(ctx context.Context) ([]core.Station, error)
	ListRequestLogsSince(ctx context.Context, since time.Time) ([]core.RequestLog, error)
}

// Scoreboard caches the latest per-(protocol, alias, stationID) score computed
// by the scoring loop. It satisfies routing.ScoreLookup so the selector can
// consult it without depending on this package.
type Scoreboard struct {
	mu     sync.RWMutex
	scores map[scoreKey]routing.StationScore
}

type scoreKey struct {
	Protocol  core.Protocol
	Alias     string
	StationID int64
}

func NewScoreboard() *Scoreboard {
	return &Scoreboard{scores: make(map[scoreKey]routing.StationScore)}
}

func (sb *Scoreboard) Score(protocol core.Protocol, alias string, stationID int64) (routing.StationScore, bool) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	v, ok := sb.scores[scoreKey{Protocol: protocol, Alias: alias, StationID: stationID}]
	return v, ok
}

func (sb *Scoreboard) replace(next map[scoreKey]routing.StationScore) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.scores = next
}

// snapshot returns a copy of the current scores. Intended for diagnostics
// (admin UI, tests).
func (sb *Scoreboard) snapshot() map[scoreKey]routing.StationScore {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	out := make(map[scoreKey]routing.StationScore, len(sb.scores))
	for k, v := range sb.scores {
		out[k] = v
	}
	return out
}

// StartScoringLoop launches a background goroutine that recomputes scores
// every ScoringInterval. It also runs one recompute immediately so a freshly
// started gateway with prior history reaches a usable scoreboard without
// waiting a full tick.
func StartScoringLoop(ctx context.Context, store scoringStore, sb *Scoreboard) {
	if store == nil || sb == nil {
		return
	}
	now := time.Now
	ticker := time.NewTicker(ScoringInterval)
	go func() {
		defer ticker.Stop()
		_ = recomputeOnce(ctx, store, sb, now())
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				_ = recomputeOnce(ctx, store, sb, t)
			}
		}
	}()
}

// recomputeOnce queries the recent request_logs window, aggregates per
// (protocol, alias, station), resolves station name to ID, and replaces the
// scoreboard atomically. Exposed for tests.
func recomputeOnce(ctx context.Context, store scoringStore, sb *Scoreboard, nowVal time.Time) error {
	stations, err := store.ListStations(ctx)
	if err != nil {
		return err
	}
	idByName := make(map[string]int64, len(stations))
	for _, station := range stations {
		idByName[station.Name] = station.ID
	}

	since := nowVal.Add(-ScoringWindow)
	logs, err := store.ListRequestLogsSince(ctx, since)
	if err != nil {
		return err
	}

	type bucket struct {
		durations []int64
		errors    int
	}
	buckets := make(map[scoreKey]*bucket)
	for _, log := range logs {
		stationID, ok := idByName[log.StationName]
		if !ok {
			// Station was renamed or deleted since the log was written — its
			// data is no longer meaningful for ranking; skip it. We don't
			// drop the log row itself (retention job owns that).
			continue
		}
		key := scoreKey{Protocol: log.Protocol, Alias: log.Alias, StationID: stationID}
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.durations = append(b.durations, log.DurationMS)
		if logIsError(log) {
			b.errors++
		}
	}

	next := make(map[scoreKey]routing.StationScore, len(buckets))
	for key, b := range buckets {
		count := len(b.durations)
		p50 := medianInt64(b.durations)
		errorRate := float64(b.errors) / float64(count)
		next[key] = routing.StationScore{
			Score:            float64(p50) * (1 + errorRate),
			HasEnoughSamples: count >= ScoringMinSample,
		}
	}
	sb.replace(next)
	return nil
}

// logIsError matches the same conditions the request log records as a failure:
// any 4xx/5xx status code or a non-empty error_kind. We deliberately count
// 4xx (e.g. 404 endpoint_not_supported, 429 rate_limited) because they
// indicate the station is not serving traffic well for this alias, even
// without triggering cooldown.
func logIsError(log core.RequestLog) bool {
	if log.ErrorKind != "" {
		return true
	}
	return log.StatusCode >= 400
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
