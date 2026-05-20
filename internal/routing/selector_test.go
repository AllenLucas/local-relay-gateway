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
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1", Enabled: true},
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

func TestCandidatesSkipDisabledMappings(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	req := core.NormalizedRequest{
		Protocol: core.ProtocolOpenAI,
		Alias:    "gpt-5",
	}

	stations := []core.Station{
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20},
		{ID: 2, Name: "station-b", Enabled: true, Priority: 10},
	}

	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: false},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].Station.ID != 2 {
		t.Fatalf("selected station ID = %d, want %d", targets[0].Station.ID, 2)
	}
}

func TestCandidatesExcludeCooldownStationUntilRecoveryMarksHealthy(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	req := core.NormalizedRequest{
		Protocol: core.ProtocolOpenAI,
		Alias:    "gpt-5",
	}

	stations := []core.Station{
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20},
	}

	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
	}

	status := core.StationStatus{
		StationID:             1,
		State:                 "cooldown",
		CooldownUntil:         now.Add(-10 * time.Second),
		ConsecutiveRecoveries: 1,
	}

	_, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{1: status})
	if err == nil {
		t.Fatal("Candidates error = nil, want non-nil while state is cooldown")
	}

	station := core.Station{
		ID:                           1,
		Name:                         "station-a",
		ConsecutiveRecoveryThreshold: 2,
	}

	recovered := selector.RecordSuccess(station, status)
	if recovered.State != "healthy" {
		t.Fatalf("state after recovery success = %q, want %q", recovered.State, "healthy")
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{1: recovered})
	if err != nil {
		t.Fatalf("Candidates after recovery error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) after recovery = %d, want 1", len(targets))
	}
}

func TestRecordFailureRespectsThresholdAndRecordSuccessRecovers(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	station := core.Station{
		ID:                           1,
		Name:                         "station-a",
		CooldownSeconds:              45,
		ConsecutiveFailureThreshold:  2,
		ConsecutiveRecoveryThreshold: 2,
	}

	status := selector.RecordFailure(station, core.StationStatus{}, "timeout")
	if status.State != "" {
		t.Fatalf("state after first failure = %q, want empty", status.State)
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("failures after first failure = %d, want 1", status.ConsecutiveFailures)
	}

	status = selector.RecordFailure(station, status, "timeout")
	if status.State != "cooldown" {
		t.Fatalf("state after threshold failure = %q, want %q", status.State, "cooldown")
	}
	if status.ConsecutiveFailures != 2 {
		t.Fatalf("failures after threshold failure = %d, want 2", status.ConsecutiveFailures)
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
