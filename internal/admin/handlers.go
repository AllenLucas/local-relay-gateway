package admin

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"

	"relay-gateway/internal/config"
	"relay-gateway/internal/core"
)

//go:embed templates/*.gohtml assets/*.js
var assets embed.FS

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
	GetStation(ctx context.Context, stationID int64) (core.Station, error)
	UpdateStation(ctx context.Context, station core.Station) error
	ListStations(ctx context.Context) ([]core.Station, error)
	ListMappings(ctx context.Context) ([]core.ModelMapping, error)
	UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error
	ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error)
	ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error)
	ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error)
	UsageByStation(ctx context.Context) ([]core.UsageRow, error)
	UsageByAlias(ctx context.Context) ([]core.UsageRow, error)
}

type Handler struct {
	store           store
	writeToken      string
	listenAddr      string
	runtimeFilePath string
	runtimeWarning  string
	setupMode       bool
	staticRoot      http.Handler
}

type Options struct {
	WriteToken      string
	ListenAddr      string
	RuntimeFilePath string
	RuntimeWarning  string
	SetupMode       bool
}

func NewHandler(store store, writeToken string) (http.Handler, error) {
	return NewHandlerWithOptions(store, Options{WriteToken: writeToken})
}

func NewHandlerWithOptions(store store, options Options) (http.Handler, error) {
	staticFS, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	listenAddr := strings.TrimSpace(options.ListenAddr)
	if listenAddr == "" {
		listenAddr = config.DefaultListenAddr
	}

	handler := &Handler{
		store:           store,
		writeToken:      options.WriteToken,
		listenAddr:      listenAddr,
		runtimeFilePath: strings.TrimSpace(options.RuntimeFilePath),
		runtimeWarning:  strings.TrimSpace(options.RuntimeWarning),
		setupMode:       options.SetupMode,
		staticRoot:      http.StripPrefix("/admin/assets/", http.FileServer(http.FS(staticFS))),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", handler.handleRoot)
	mux.HandleFunc("/admin/", handler.handleRoot)
	mux.HandleFunc("/admin/setup", handler.handleSetup)
	mux.HandleFunc("/admin/runtime", handler.handleRuntime)
	mux.HandleFunc("/admin/stations", handler.handleStations)
	mux.HandleFunc("/admin/stations/edit", handler.handleStationEdit)
	mux.HandleFunc("/admin/stations/update", handler.handleStationUpdate)
	mux.HandleFunc("/admin/mappings", handler.handleMappings)
	mux.HandleFunc("/admin/status", handler.handleStatus)
	mux.HandleFunc("/admin/logs", handler.handleLogs)
	mux.Handle("/admin/assets/", handler.staticRoot)
	return mux, nil
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
}

func (h *Handler) renderPage(w http.ResponseWriter, page string, data any) error {
	templates, err := template.ParseFS(assets, "templates/layout.gohtml", page)
	if err != nil {
		return err
	}
	return templates.ExecuteTemplate(w, "layout", data)
}

func (h *Handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if h.hasValidRuntimeFile() {
			http.Redirect(w, r, "/admin/runtime", http.StatusSeeOther)
			return
		}
		data := SetupPage{
			Title:           "Setup",
			WriteToken:      h.writeToken,
			RuntimeFilePath: h.runtimeFilePath,
			RuntimeWarning:  h.runtimeWarning,
		}
		if err := h.renderPage(w, "templates/setup.gohtml", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		localAPIKey, err := parseRequiredText(r.Form.Get("local_api_key"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if h.runtimeFilePath == "" {
			http.Error(w, "runtime file path is not configured", http.StatusInternalServerError)
			return
		}
		if err := config.SaveRuntimeFile(h.runtimeFilePath, config.RuntimeFile{LocalAPIKey: localAPIKey}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleRuntime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		localAPIKey := ""
		runtimeError := ""
		if h.runtimeFilePath != "" {
			if runtimeFile, err := config.LoadRuntimeFile(h.runtimeFilePath); err != nil {
				runtimeError = "Runtime file could not be loaded: " + err.Error()
			} else {
				localAPIKey = runtimeFile.LocalAPIKey
			}
		}
		localBaseURL := localBaseURL(h.listenAddr)
		data := RuntimePage{
			Title:           "Runtime",
			WriteToken:      h.writeToken,
			ListenAddr:      h.listenAddr,
			OpenAIBaseURL:   localBaseURL + "/openai/v1",
			AnthropicURL:    localBaseURL + "/anthropic",
			LocalAPIKey:     localAPIKey,
			RuntimeError:    runtimeError,
			RuntimeFilePath: h.runtimeFilePath,
			Saved:           r.URL.Query().Get("saved") == "1",
		}
		if err := h.renderPage(w, "templates/runtime.gohtml", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		localAPIKey, err := parseRequiredText(r.Form.Get("local_api_key"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if h.runtimeFilePath == "" {
			http.Error(w, "runtime file path is not configured", http.StatusInternalServerError)
			return
		}
		if err := config.SaveRuntimeFile(h.runtimeFilePath, config.RuntimeFile{LocalAPIKey: localAPIKey}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/runtime?saved=1", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) hasValidRuntimeFile() bool {
	if h.runtimeFilePath == "" {
		return false
	}
	runtimeFile, err := config.LoadRuntimeFile(h.runtimeFilePath)
	return err == nil && strings.TrimSpace(runtimeFile.LocalAPIKey) != ""
}

func localBaseURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		if strings.HasPrefix(listenAddr, ":") {
			return "http://127.0.0.1" + listenAddr
		}
		return "http://" + listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (h *Handler) handleStations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		name, err := parseRequiredText(r.Form.Get("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		openAIBaseURL := strings.TrimSpace(r.Form.Get("openai_base_url"))
		openAIAPIKey := strings.TrimSpace(r.Form.Get("openai_api_key"))
		anthropicBaseURL := strings.TrimSpace(r.Form.Get("anthropic_base_url"))
		anthropicAPIKey := strings.TrimSpace(r.Form.Get("anthropic_api_key"))
		if err := validateStationProtocols(openAIBaseURL, openAIAPIKey, anthropicBaseURL, anthropicAPIKey); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		priority, err := parsePositiveInt(r.Form.Get("priority"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cooldownSeconds, err := parsePositiveInt(r.Form.Get("cooldown_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		healthInterval, err := parsePositiveInt(r.Form.Get("health_check_interval_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		healthTimeout, err := parsePositiveInt(r.Form.Get("health_check_timeout_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		failureThreshold, err := parsePositiveInt(r.Form.Get("consecutive_failure_threshold"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		recoveryThreshold, err := parsePositiveInt(r.Form.Get("consecutive_recovery_threshold"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = h.store.CreateStation(r.Context(), core.Station{
			Name:                         name,
			Enabled:                      r.Form.Get("enabled") == "on",
			Priority:                     priority,
			CooldownSeconds:              cooldownSeconds,
			HealthCheckIntervalSeconds:   healthInterval,
			HealthCheckTimeoutSeconds:    healthTimeout,
			ConsecutiveFailureThreshold:  failureThreshold,
			ConsecutiveRecoveryThreshold: recoveryThreshold,
			OpenAIBaseURL:                openAIBaseURL,
			OpenAIAPIKey:                 openAIAPIKey,
			AnthropicBaseURL:             anthropicBaseURL,
			AnthropicAPIKey:              anthropicAPIKey,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
		return
	}

	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := StationsPage{
		Title:                   "Stations",
		WriteToken:              h.writeToken,
		Stations:                stations,
		ShowOnboarding:          len(stations) == 0,
		DefaultListenAddr:       "127.0.0.1:8787",
		ExampleOpenAIBaseURL:    "https://relay-a.example.invalid/openai",
		ExampleAnthropicBaseURL: "https://relay-a.example.invalid/anthropic",
		ExampleOpenAIAlias:      "gpt-5",
		ExampleAnthropicAlias:   "claude-sonnet",
		ExampleOpenAIModel:      "gpt-5.1",
		ExampleAnthropicModel:   "claude-sonnet-4-5",
	}
	if err := h.renderPage(w, "templates/stations.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleStationEdit(w http.ResponseWriter, r *http.Request) {
	stationID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if err != nil || stationID <= 0 {
		http.Error(w, "invalid station id", http.StatusBadRequest)
		return
	}

	station, err := h.store.GetStation(r.Context(), stationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := StationsPage{
		Title:                   "Stations",
		WriteToken:              h.writeToken,
		Stations:                stations,
		EditStation:             &station,
		ShowOnboarding:          len(stations) == 0,
		DefaultListenAddr:       "127.0.0.1:8787",
		ExampleOpenAIBaseURL:    "https://relay-a.example.invalid/openai",
		ExampleAnthropicBaseURL: "https://relay-a.example.invalid/anthropic",
		ExampleOpenAIAlias:      "gpt-5",
		ExampleAnthropicAlias:   "claude-sonnet",
		ExampleOpenAIModel:      "gpt-5.1",
		ExampleAnthropicModel:   "claude-sonnet-4-5",
	}
	if err := h.renderPage(w, "templates/stations.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleStationUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.isValidWriteToken(r.Form.Get("write_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	stationID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("id")), 10, 64)
	if err != nil || stationID <= 0 {
		http.Error(w, "invalid station id", http.StatusBadRequest)
		return
	}

	station, err := parseStationForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	station.ID = stationID

	if err := h.store.UpdateStation(r.Context(), station); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
}

func (h *Handler) handleMappings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		protocol, err := parseRequiredText(r.Form.Get("protocol"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		alias, err := parseRequiredText(r.Form.Get("alias"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upstreamModel, err := parseRequiredText(r.Form.Get("upstream_model"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		stationID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("station_id")), 10, 64)
		if err != nil || stationID <= 0 {
			http.Error(w, "invalid station_id", http.StatusBadRequest)
			return
		}
		stations, err := h.store.ListStations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !stationExists(stations, stationID) {
			http.Error(w, "invalid station_id", http.StatusBadRequest)
			return
		}

		err = h.store.UpsertModelMapping(r.Context(), core.ModelMapping{
			StationID:     stationID,
			Protocol:      core.Protocol(protocol),
			Alias:         alias,
			UpstreamModel: upstreamModel,
			Enabled:       r.Form.Get("enabled") == "on",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/admin/mappings", http.StatusSeeOther)
		return
	}

	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mappings, err := h.store.ListMappings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := MappingsPage{
		Title:      "Mappings",
		WriteToken: h.writeToken,
		Stations:   stations,
		Mappings:   mappings,
	}
	if err := h.renderPage(w, "templates/mappings.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	statuses, err := h.store.ListStationStatuses(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := StatusPage{
		Title:    "Status",
		Stations: stations,
		Statuses: statuses,
	}
	if err := h.renderPage(w, "templates/status.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	requestLogs, err := h.store.ListRequestLogs(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	failoverEvents, err := h.store.ListFailoverEvents(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	usageByStation, err := h.store.UsageByStation(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	usageByAlias, err := h.store.UsageByAlias(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := LogsPage{
		Title:          "Logs",
		RequestLogs:    requestLogs,
		FailoverEvents: failoverEvents,
		UsageByStation: usageByStation,
		UsageByAlias:   usageByAlias,
	}
	if err := h.renderPage(w, "templates/logs.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) isValidWriteToken(value string) bool {
	return h.writeToken != "" && value == h.writeToken
}

func parseStationForm(r *http.Request) (core.Station, error) {
	name, err := parseRequiredText(r.Form.Get("name"))
	if err != nil {
		return core.Station{}, err
	}

	openAIBaseURL := strings.TrimSpace(r.Form.Get("openai_base_url"))
	openAIAPIKey := strings.TrimSpace(r.Form.Get("openai_api_key"))
	anthropicBaseURL := strings.TrimSpace(r.Form.Get("anthropic_base_url"))
	anthropicAPIKey := strings.TrimSpace(r.Form.Get("anthropic_api_key"))
	if err := validateStationProtocols(openAIBaseURL, openAIAPIKey, anthropicBaseURL, anthropicAPIKey); err != nil {
		return core.Station{}, err
	}

	priority, err := parsePositiveInt(r.Form.Get("priority"))
	if err != nil {
		return core.Station{}, err
	}
	cooldownSeconds, err := parsePositiveInt(r.Form.Get("cooldown_seconds"))
	if err != nil {
		return core.Station{}, err
	}
	healthInterval, err := parsePositiveInt(r.Form.Get("health_check_interval_seconds"))
	if err != nil {
		return core.Station{}, err
	}
	healthTimeout, err := parsePositiveInt(r.Form.Get("health_check_timeout_seconds"))
	if err != nil {
		return core.Station{}, err
	}
	failureThreshold, err := parsePositiveInt(r.Form.Get("consecutive_failure_threshold"))
	if err != nil {
		return core.Station{}, err
	}
	recoveryThreshold, err := parsePositiveInt(r.Form.Get("consecutive_recovery_threshold"))
	if err != nil {
		return core.Station{}, err
	}

	return core.Station{
		Name:                         name,
		Enabled:                      r.Form.Get("enabled") == "on",
		Priority:                     priority,
		CooldownSeconds:              cooldownSeconds,
		HealthCheckIntervalSeconds:   healthInterval,
		HealthCheckTimeoutSeconds:    healthTimeout,
		ConsecutiveFailureThreshold:  failureThreshold,
		ConsecutiveRecoveryThreshold: recoveryThreshold,
		OpenAIBaseURL:                openAIBaseURL,
		OpenAIAPIKey:                 openAIAPIKey,
		AnthropicBaseURL:             anthropicBaseURL,
		AnthropicAPIKey:              anthropicAPIKey,
	}, nil
}

func validateStationProtocols(openAIBaseURL string, openAIAPIKey string, anthropicBaseURL string, anthropicAPIKey string) error {
	hasOpenAIBase := openAIBaseURL != ""
	hasOpenAIKey := openAIAPIKey != ""
	if hasOpenAIBase != hasOpenAIKey {
		return errors.New("openai base url and api key must be provided together")
	}

	hasAnthropicBase := anthropicBaseURL != ""
	hasAnthropicKey := anthropicAPIKey != ""
	if hasAnthropicBase != hasAnthropicKey {
		return errors.New("anthropic base url and api key must be provided together")
	}

	if !hasOpenAIBase && !hasAnthropicBase {
		return errors.New("at least one protocol must be configured")
	}
	return nil
}

func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, errors.New("invalid numeric input")
	}
	if parsed <= 0 {
		return 0, errors.New("numeric input must be greater than zero")
	}
	return parsed, nil
}

func stationExists(stations []core.Station, stationID int64) bool {
	for _, station := range stations {
		if station.ID == stationID {
			return true
		}
	}
	return false
}

func parseRequiredText(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("required field is empty")
	}
	return trimmed, nil
}
