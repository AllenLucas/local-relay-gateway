package jobs

import (
	"context"
	"testing"
	"time"

	"relay-gateway/internal/core"
)

type fakeScoringStore struct {
	stations []core.Station
	logs     []core.RequestLog
}

func (f *fakeScoringStore) ListStations(_ context.Context) ([]core.Station, error) {
	return f.stations, nil
}

func (f *fakeScoringStore) ListRequestLogsSince(_ context.Context, since time.Time) ([]core.RequestLog, error) {
	out := make([]core.RequestLog, 0, len(f.logs))
	for _, log := range f.logs {
		if !log.CreatedAt.Before(since) {
			out = append(out, log)
		}
	}
	return out, nil
}

func makeStation(id int64, name string) core.Station {
	return core.Station{ID: id, Name: name, Enabled: true}
}

func makeLog(protocol core.Protocol, alias, station string, duration int64, status int, errorKind string, at time.Time) core.RequestLog {
	return core.RequestLog{
		Protocol:    protocol,
		Alias:       alias,
		StationName: station,
		StatusCode:  status,
		DurationMS:  duration,
		ErrorKind:   errorKind,
		CreatedAt:   at,
	}
}

func TestRecomputeOnceP50AndErrorRate(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeScoringStore{
		stations: []core.Station{makeStation(1, "alpha")},
	}
	// 10 samples: durations 100..1000, 2 errors. p50 (index 5) = 600. err rate = 0.2.
	// expected score = 600 * 1.2 = 720.
	durations := []int64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	for i, d := range durations {
		status := 200
		var kind string
		if i == 0 || i == 1 {
			status = 500
			kind = "upstream_error"
		}
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", d, status, kind, now.Add(-5*time.Minute)))
	}
	sb := NewScoreboard()
	if err := recomputeOnce(context.Background(), store, sb, now); err != nil {
		t.Fatalf("recomputeOnce error = %v", err)
	}
	score, ok := sb.Score(core.ProtocolOpenAI, "gpt-5", 1)
	if !ok {
		t.Fatalf("Score missing for (openai, gpt-5, 1)")
	}
	if !score.HasEnoughSamples {
		t.Fatalf("HasEnoughSamples = false, want true with 10 samples (min threshold)")
	}
	if score.Score != 720 {
		t.Fatalf("Score = %v, want 720 (p50=600 × (1+0.2))", score.Score)
	}
}

func TestRecomputeOnceMarksLowSampleStationsInsufficient(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeScoringStore{
		stations: []core.Station{makeStation(1, "alpha")},
	}
	for i := 0; i < ScoringMinSample-1; i++ {
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", 100, 200, "", now.Add(-time.Minute)))
	}
	sb := NewScoreboard()
	if err := recomputeOnce(context.Background(), store, sb, now); err != nil {
		t.Fatalf("recomputeOnce error = %v", err)
	}
	score, ok := sb.Score(core.ProtocolOpenAI, "gpt-5", 1)
	if !ok {
		t.Fatalf("Score should still be recorded even with insufficient samples")
	}
	if score.HasEnoughSamples {
		t.Fatalf("HasEnoughSamples = true, want false with %d samples (< %d)", ScoringMinSample-1, ScoringMinSample)
	}
}

func TestRecomputeOnceIgnoresLogsOutsideWindow(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeScoringStore{
		stations: []core.Station{makeStation(1, "alpha")},
	}
	// One ancient log + nine fresh ones → in-window count is 9 → insufficient.
	store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", 50, 200, "", now.Add(-2*ScoringWindow)))
	for i := 0; i < 9; i++ {
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", 500, 200, "", now.Add(-time.Minute)))
	}
	sb := NewScoreboard()
	if err := recomputeOnce(context.Background(), store, sb, now); err != nil {
		t.Fatalf("recomputeOnce error = %v", err)
	}
	score, _ := sb.Score(core.ProtocolOpenAI, "gpt-5", 1)
	if score.HasEnoughSamples {
		t.Fatalf("HasEnoughSamples = true, want false (ancient log should be excluded)")
	}
}

func TestRecomputeOncePerAliasStationDimension(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeScoringStore{
		stations: []core.Station{
			makeStation(1, "alpha"),
			makeStation(2, "beta"),
		},
	}
	// alpha is fast on gpt-5 (100ms), slow on claude (900ms).
	// beta opposite. Verify per-(alias, station) bookkeeping.
	for i := 0; i < ScoringMinSample; i++ {
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", 100, 200, "", now.Add(-time.Minute)))
		store.logs = append(store.logs, makeLog(core.ProtocolAnthropic, "claude", "alpha", 900, 200, "", now.Add(-time.Minute)))
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "beta", 800, 200, "", now.Add(-time.Minute)))
		store.logs = append(store.logs, makeLog(core.ProtocolAnthropic, "claude", "beta", 150, 200, "", now.Add(-time.Minute)))
	}
	sb := NewScoreboard()
	if err := recomputeOnce(context.Background(), store, sb, now); err != nil {
		t.Fatalf("recomputeOnce error = %v", err)
	}
	cases := []struct {
		protocol  core.Protocol
		alias     string
		stationID int64
		wantScore float64
	}{
		{core.ProtocolOpenAI, "gpt-5", 1, 100},
		{core.ProtocolAnthropic, "claude", 1, 900},
		{core.ProtocolOpenAI, "gpt-5", 2, 800},
		{core.ProtocolAnthropic, "claude", 2, 150},
	}
	for _, c := range cases {
		score, ok := sb.Score(c.protocol, c.alias, c.stationID)
		if !ok {
			t.Fatalf("missing score for %s/%s/%d", c.protocol, c.alias, c.stationID)
		}
		if score.Score != c.wantScore {
			t.Fatalf("score for %s/%s/%d = %v, want %v", c.protocol, c.alias, c.stationID, score.Score, c.wantScore)
		}
	}
}

func TestRecomputeOnceSkipsLogsForUnknownStation(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeScoringStore{
		stations: []core.Station{makeStation(1, "alpha")},
	}
	for i := 0; i < ScoringMinSample; i++ {
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "alpha", 100, 200, "", now.Add(-time.Minute)))
		store.logs = append(store.logs, makeLog(core.ProtocolOpenAI, "gpt-5", "ghost-station", 999, 200, "", now.Add(-time.Minute)))
	}
	sb := NewScoreboard()
	if err := recomputeOnce(context.Background(), store, sb, now); err != nil {
		t.Fatalf("recomputeOnce error = %v", err)
	}
	if _, ok := sb.Score(core.ProtocolOpenAI, "gpt-5", 1); !ok {
		t.Fatal("expected score for known station to be present")
	}
	if got := sb.snapshot(); len(got) != 1 {
		t.Fatalf("snapshot size = %d, want 1 (ghost station logs should be skipped)", len(got))
	}
}
