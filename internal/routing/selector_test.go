package routing_test

import (
	"testing"
	"time"

	"relay-gateway/internal/core"
	"relay-gateway/internal/routing"
)

func TestCandidatesPreferHealthyPrimaryAndSkipCooldown(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	req := core.NormalizedRequest{
		Protocol: core.ProtocolOpenAI,
		Alias:    "gpt-5",
	}

	stations := []core.Station{
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20, CooldownSeconds: 30},
		{ID: 2, Name: "station-b", Enabled: true, Priority: 10, CooldownSeconds: 30},
	}

	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5"},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1"},
	}

	statuses := map[int64]core.StationStatus{
		1: {StationID: 1, State: "cooldown", CooldownUntil: now.Add(10 * time.Second)},
		2: {StationID: 2, State: "healthy"},
	}

	targets, err := selector.Candidates(req, stations, mappings, statuses)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].Station.Name != "station-b" {
		t.Fatalf("selected station = %q, want %q", targets[0].Station.Name, "station-b")
	}
}

func TestRecordFailureStartsCooldownAndRecordSuccessRecovers(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	station := core.Station{
		ID:                           1,
		Name:                         "station-a",
		CooldownSeconds:              45,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
	}

	status := selector.RecordFailure(station, core.StationStatus{}, "timeout")
	if status.State != "cooldown" {
		t.Fatalf("failure state = %q, want %q", status.State, "cooldown")
	}

	status = selector.RecordSuccess(station, status)
	if status.State != "cooldown" {
		t.Fatalf("state after first success = %q, want %q", status.State, "cooldown")
	}

	status = selector.RecordSuccess(station, status)
	if status.State != "healthy" {
		t.Fatalf("state after second success = %q, want %q", status.State, "healthy")
	}
}
