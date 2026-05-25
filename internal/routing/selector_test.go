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
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20, CooldownSeconds: 30, OpenAIBaseURL: "https://a.example.com/openai", OpenAIAPIKey: "OPENAI_A"},
		{ID: 2, Name: "station-b", Enabled: true, Priority: 10, CooldownSeconds: 30, OpenAIBaseURL: "https://b.example.com/openai", OpenAIAPIKey: "OPENAI_B"},
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
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20, OpenAIBaseURL: "https://a.example.com/openai", OpenAIAPIKey: "OPENAI_A"},
		{ID: 2, Name: "station-b", Enabled: true, Priority: 10, OpenAIBaseURL: "https://b.example.com/openai", OpenAIAPIKey: "OPENAI_B"},
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
		{ID: 1, Name: "station-a", Enabled: true, Priority: 20, OpenAIBaseURL: "https://a.example.com/openai", OpenAIAPIKey: "OPENAI_A"},
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
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
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

func TestCandidatesSkipStationsWithoutRequestedProtocolConfig(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	selector := routing.NewSelector(func() time.Time { return now })

	req := core.NormalizedRequest{
		Protocol: core.ProtocolAnthropic,
		Alias:    "claude-sonnet",
	}

	stations := []core.Station{
		{
			ID:            1,
			Name:          "openai-only",
			Enabled:       true,
			Priority:      20,
			OpenAIBaseURL: "https://a.example.com/openai",
			OpenAIAPIKey:  "OPENAI_A",
		},
		{
			ID:               2,
			Name:             "anthropic-only",
			Enabled:          true,
			Priority:         10,
			AnthropicBaseURL: "https://b.example.com/anthropic",
			AnthropicAPIKey:  "ANTHROPIC_B",
		},
	}

	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolAnthropic, Alias: "claude-sonnet", UpstreamModel: "claude-sonnet-4-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolAnthropic, Alias: "claude-sonnet", UpstreamModel: "claude-sonnet-4-5", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].Station.Name != "anthropic-only" {
		t.Fatalf("selected station = %q, want %q", targets[0].Station.Name, "anthropic-only")
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

type fakeScoreLookup map[string]routing.StationScore

func (f fakeScoreLookup) Score(protocol core.Protocol, alias string, stationID int64) (routing.StationScore, bool) {
	key := string(protocol) + "|" + alias + "|" + intKey(stationID)
	v, ok := f[key]
	return v, ok
}

func intKey(id int64) string {
	const digits = "0123456789"
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	idx := len(buf)
	for id > 0 {
		idx--
		buf[idx] = digits[id%10]
		id /= 10
	}
	return string(buf[idx:])
}

func makeOpenAIStation(id int64, name string, priority int) core.Station {
	return core.Station{
		ID:            id,
		Name:          name,
		Enabled:       true,
		Priority:      priority,
		OpenAIBaseURL: "https://" + name + ".example.com/openai",
		OpenAIAPIKey:  "OPENAI_" + name,
	}
}

func TestCandidatesManualOutranksAuto(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	scores := fakeScoreLookup{
		"openai|gpt-5|2": {Score: 10, HasEnoughSamples: true}, // auto, very fast
	}
	selector := routing.NewSelectorWithScores(func() time.Time { return now }, scores)

	req := core.NormalizedRequest{Protocol: core.ProtocolOpenAI, Alias: "gpt-5"}
	stations := []core.Station{
		makeOpenAIStation(1, "manual-slow", 5),
		makeOpenAIStation(2, "auto-fast", 0),
	}
	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].Station.Name != "manual-slow" {
		t.Fatalf("first = %q, want manual-slow (manual tier always before auto, regardless of score)", targets[0].Station.Name)
	}
	if targets[1].Station.Name != "auto-fast" {
		t.Fatalf("second = %q, want auto-fast", targets[1].Station.Name)
	}
}

func TestCandidatesAutoTierSortsByScore(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	scores := fakeScoreLookup{
		"openai|gpt-5|1": {Score: 800, HasEnoughSamples: true},
		"openai|gpt-5|2": {Score: 200, HasEnoughSamples: true},
		"openai|gpt-5|3": {Score: 500, HasEnoughSamples: true},
	}
	selector := routing.NewSelectorWithScores(func() time.Time { return now }, scores)

	req := core.NormalizedRequest{Protocol: core.ProtocolOpenAI, Alias: "gpt-5"}
	stations := []core.Station{
		makeOpenAIStation(1, "slow", 0),
		makeOpenAIStation(2, "fast", 0),
		makeOpenAIStation(3, "medium", 0),
	}
	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 3, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("len(targets) = %d, want 3", len(targets))
	}
	wantOrder := []string{"fast", "medium", "slow"}
	for i, want := range wantOrder {
		if targets[i].Station.Name != want {
			t.Fatalf("targets[%d] = %q, want %q (auto tier should sort by score asc)", i, targets[i].Station.Name, want)
		}
	}
}

func TestCandidatesAutoTierPushesUnscoredStationsToTail(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	scores := fakeScoreLookup{
		"openai|gpt-5|1": {Score: 800, HasEnoughSamples: true},
		"openai|gpt-5|2": {Score: 200, HasEnoughSamples: true},
		// station 3 has no entry → treated as samples-insufficient
		// station 4 has entry but HasEnoughSamples=false → also tail
		"openai|gpt-5|4": {Score: 50, HasEnoughSamples: false},
	}
	selector := routing.NewSelectorWithScores(func() time.Time { return now }, scores)

	req := core.NormalizedRequest{Protocol: core.ProtocolOpenAI, Alias: "gpt-5"}
	stations := []core.Station{
		makeOpenAIStation(1, "slow-scored", 0),
		makeOpenAIStation(2, "fast-scored", 0),
		makeOpenAIStation(3, "no-samples", 0),
		makeOpenAIStation(4, "low-samples", 0),
	}
	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 3, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 4, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	wantOrder := []string{"fast-scored", "slow-scored", "no-samples", "low-samples"}
	if len(targets) != len(wantOrder) {
		t.Fatalf("len(targets) = %d, want %d", len(targets), len(wantOrder))
	}
	for i, want := range wantOrder {
		if targets[i].Station.Name != want {
			t.Fatalf("targets[%d] = %q, want %q", i, targets[i].Station.Name, want)
		}
	}
}

func TestCandidatesManualTierSortsByPriorityDesc(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	scores := fakeScoreLookup{
		"openai|gpt-5|3": {Score: 100, HasEnoughSamples: true},
	}
	selector := routing.NewSelectorWithScores(func() time.Time { return now }, scores)

	req := core.NormalizedRequest{Protocol: core.ProtocolOpenAI, Alias: "gpt-5"}
	stations := []core.Station{
		makeOpenAIStation(1, "low-prio", 1),
		makeOpenAIStation(2, "high-prio", 10),
		makeOpenAIStation(3, "auto", 0),
	}
	mappings := []core.ModelMapping{
		{StationID: 1, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 2, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
		{StationID: 3, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true},
	}

	targets, err := selector.Candidates(req, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	wantOrder := []string{"high-prio", "low-prio", "auto"}
	if len(targets) != len(wantOrder) {
		t.Fatalf("len(targets) = %d, want %d", len(targets), len(wantOrder))
	}
	for i, want := range wantOrder {
		if targets[i].Station.Name != want {
			t.Fatalf("targets[%d] = %q, want %q", i, targets[i].Station.Name, want)
		}
	}
}
