package admin

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"relay-gateway/internal/config"
	"relay-gateway/internal/configsync"
	"relay-gateway/internal/core"
)

//go:embed templates/*.gohtml assets/*.js
var assets embed.FS

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
	GetStation(ctx context.Context, stationID int64) (core.Station, error)
	UpdateStation(ctx context.Context, station core.Station) error
	DeleteStation(ctx context.Context, stationID int64) error
	ListStations(ctx context.Context) ([]core.Station, error)
	ListMappings(ctx context.Context) ([]core.ModelMapping, error)
	UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error
	GetMapping(ctx context.Context, mappingID int64) (core.ModelMapping, error)
	UpdateModelMapping(ctx context.Context, mapping core.ModelMapping) error
	DeleteModelMapping(ctx context.Context, mappingID int64) error
	ExportConfigSnapshot(ctx context.Context) (configsync.Snapshot, error)
	ApplyConfigSnapshot(ctx context.Context, snapshot configsync.Snapshot) (configsync.ApplyResult, error)
	ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error)
	ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error)
	ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error)
	ListUpstreamErrorLogs(ctx context.Context, limit int) ([]core.UpstreamErrorLog, error)
	ListRecentUpstreamErrorLogsByStation(ctx context.Context, limitPerStation int) (map[string][]core.UpstreamErrorLog, error)
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
	now             func() time.Time
	staticRoot      http.Handler
}

type Options struct {
	WriteToken      string
	ListenAddr      string
	RuntimeFilePath string
	RuntimeWarning  string
	SetupMode       bool
	Now             func() time.Time
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
		now:             options.Now,
		staticRoot:      http.StripPrefix("/admin/assets/", http.FileServer(http.FS(staticFS))),
	}
	if handler.now == nil {
		handler.now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", handler.handleRoot)
	mux.HandleFunc("/admin/", handler.handleRoot)
	mux.HandleFunc("/admin/setup", handler.handleSetup)
	mux.HandleFunc("/admin/runtime", handler.handleRuntime)
	mux.HandleFunc("/admin/sync", handler.handleSync)
	mux.HandleFunc("/admin/sync/upload", handler.handleSyncUpload)
	mux.HandleFunc("/admin/sync/pull", handler.handleSyncPull)
	mux.HandleFunc("/admin/stations", handler.handleStations)
	mux.HandleFunc("/admin/stations/edit", handler.handleStationEdit)
	mux.HandleFunc("/admin/stations/update", handler.handleStationUpdate)
	mux.HandleFunc("/admin/stations/delete", handler.handleStationDelete)
	mux.HandleFunc("/admin/mappings", handler.handleMappings)
	mux.HandleFunc("/admin/mappings/edit", handler.handleMappingEdit)
	mux.HandleFunc("/admin/mappings/update", handler.handleMappingUpdate)
	mux.HandleFunc("/admin/mappings/delete", handler.handleMappingDelete)
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

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := SyncPage{
		Title:      "Sync",
		WriteToken: h.writeToken,
		DeviceName: defaultDeviceName(),
		Uploaded:   r.URL.Query().Get("uploaded") == "1",
		Pulled:     r.URL.Query().Get("pulled") == "1",
		RemoteFile: r.URL.Query().Get("file"),
		Result:     r.URL.Query().Get("result"),
	}
	if err := h.renderPage(w, "templates/sync.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleSyncUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := h.parseSyncForm(r)
	if err != nil {
		if errors.Is(err, errForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := h.store.ExportConfigSnapshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := h.now().UTC()
	snapshot.ExportedAt = now
	snapshot.DeviceName = cfg.DeviceName
	body, err := configsync.EncodeSnapshot(snapshot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cfg.Now = func() time.Time { return now }
	remoteFile, err := configsync.NewWebDAVClient(cfg).UploadSnapshot(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/admin/sync?uploaded=1&file="+urlQueryEscape(remoteFile.Path), http.StatusSeeOther)
}

func (h *Handler) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := h.parseSyncForm(r)
	if err != nil {
		if errors.Is(err, errForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	remoteFile, err := configsync.NewWebDAVClient(cfg).DownloadLatestSnapshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	snapshot, err := configsync.DecodeSnapshot(remoteFile.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.store.ApplyConfigSnapshot(r.Context(), snapshot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/sync?pulled=1&file="+urlQueryEscape(remoteFile.Path)+"&result="+urlQueryEscape(syncResultText(result)), http.StatusSeeOther)
}

func (h *Handler) parseSyncForm(r *http.Request) (configsync.WebDAVConfig, error) {
	if err := r.ParseForm(); err != nil {
		return configsync.WebDAVConfig{}, err
	}
	if !h.isValidWriteToken(r.Form.Get("write_token")) {
		return configsync.WebDAVConfig{}, errForbidden
	}
	webDAVURL, err := parseRequiredText(r.Form.Get("webdav_url"))
	if err != nil {
		return configsync.WebDAVConfig{}, err
	}
	deviceName := strings.TrimSpace(r.Form.Get("device_name"))
	if deviceName == "" {
		deviceName = defaultDeviceName()
	}
	return configsync.WebDAVConfig{
		BaseURL:    webDAVURL,
		Username:   strings.TrimSpace(r.Form.Get("username")),
		Password:   strings.TrimSpace(r.Form.Get("password")),
		DeviceName: deviceName,
	}, nil
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
		if h.stationNameExists(r.Context(), name, 0) {
			http.Error(w, "station name already exists", http.StatusBadRequest)
			return
		}

		priority, err := parseNonNegativeInt(r.Form.Get("priority"))
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
	if h.stationNameExists(r.Context(), station.Name, stationID) {
		http.Error(w, "station name already exists", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateStation(r.Context(), station); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
}

func (h *Handler) handleStationDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.store.DeleteStation(r.Context(), stationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "station not found", http.StatusNotFound)
			return
		}
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

		mapping, err := parseMappingForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		stations, err := h.store.ListStations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !stationExists(stations, mapping.StationID) {
			http.Error(w, "invalid station_id", http.StatusBadRequest)
			return
		}

		if err := h.store.UpsertModelMapping(r.Context(), mapping); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/admin/mappings", http.StatusSeeOther)
		return
	}

	if err := h.renderMappingsPage(w, r, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleMappingEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mappingID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if err != nil || mappingID <= 0 {
		http.Error(w, "invalid mapping id", http.StatusBadRequest)
		return
	}

	mapping, err := h.store.GetMapping(r.Context(), mappingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "mapping not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.renderMappingsPage(w, r, &mapping); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleMappingUpdate(w http.ResponseWriter, r *http.Request) {
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

	mappingID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("id")), 10, 64)
	if err != nil || mappingID <= 0 {
		http.Error(w, "invalid mapping id", http.StatusBadRequest)
		return
	}

	mapping, err := parseMappingForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mapping.ID = mappingID

	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !stationExists(stations, mapping.StationID) {
		http.Error(w, "invalid station_id", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateModelMapping(r.Context(), mapping); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "mapping not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/mappings", http.StatusSeeOther)
}

func (h *Handler) handleMappingDelete(w http.ResponseWriter, r *http.Request) {
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
	mappingID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("id")), 10, 64)
	if err != nil || mappingID <= 0 {
		http.Error(w, "invalid mapping id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteModelMapping(r.Context(), mappingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "mapping not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/mappings", http.StatusSeeOther)
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
	recentErrors, err := h.store.ListRecentUpstreamErrorLogsByStation(r.Context(), 3)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := StatusPage{
		Title:                "Status",
		Stations:             stations,
		Statuses:             statuses,
		RecentUpstreamErrors: recentErrors,
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
	upstreamErrors, err := h.store.ListUpstreamErrorLogs(r.Context(), 50)
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
		UpstreamErrors: upstreamErrors,
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

var errForbidden = errors.New("forbidden")

func (h *Handler) stationNameExists(ctx context.Context, name string, exceptID int64) bool {
	stations, err := h.store.ListStations(ctx)
	if err != nil {
		return false
	}
	for _, station := range stations {
		if station.Name == name && station.ID != exceptID {
			return true
		}
	}
	return false
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

	priority, err := parseNonNegativeInt(r.Form.Get("priority"))
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

func parseMappingForm(r *http.Request) (core.ModelMapping, error) {
	protocol, err := parseRequiredText(r.Form.Get("protocol"))
	if err != nil {
		return core.ModelMapping{}, err
	}
	alias, err := parseRequiredText(r.Form.Get("alias"))
	if err != nil {
		return core.ModelMapping{}, err
	}
	upstreamModel, err := parseRequiredText(r.Form.Get("upstream_model"))
	if err != nil {
		return core.ModelMapping{}, err
	}
	stationID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("station_id")), 10, 64)
	if err != nil || stationID <= 0 {
		return core.ModelMapping{}, errors.New("invalid station_id")
	}

	return core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.Protocol(protocol),
		Alias:         alias,
		UpstreamModel: upstreamModel,
		Enabled:       r.Form.Get("enabled") == "on",
	}, nil
}

func (h *Handler) renderMappingsPage(w http.ResponseWriter, r *http.Request, editMapping *core.ModelMapping) error {
	stations, err := h.store.ListStations(r.Context())
	if err != nil {
		return err
	}
	mappings, err := h.store.ListMappings(r.Context())
	if err != nil {
		return err
	}

	stationNameByID := make(map[int64]string, len(stations))
	for _, station := range stations {
		stationNameByID[station.ID] = station.Name
	}

	data := MappingsPage{
		Title:           "Mappings",
		WriteToken:      h.writeToken,
		Stations:        stations,
		StationNameByID: stationNameByID,
		Mappings:        mappings,
		EditMapping:     editMapping,
	}
	return h.renderPage(w, "templates/mappings.gohtml", data)
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

// parseNonNegativeInt allows 0 as a sentinel value. Used for station Priority
// where 0 means "auto" (let the scoring job rank this station dynamically) and
// any positive value locks the station to manual priority order.
func parseNonNegativeInt(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, errors.New("invalid numeric input")
	}
	if parsed < 0 {
		return 0, errors.New("numeric input must be zero or greater")
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

func defaultDeviceName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "local-device"
	}
	return strings.TrimSpace(name)
}

func syncResultText(result configsync.ApplyResult) string {
	return "stations created=" + strconv.Itoa(result.CreatedStations) +
		" updated=" + strconv.Itoa(result.UpdatedStations) +
		" deleted=" + strconv.Itoa(result.DeletedStations) +
		"; mappings created=" + strconv.Itoa(result.CreatedMappings) +
		" updated=" + strconv.Itoa(result.UpdatedMappings) +
		" deleted=" + strconv.Itoa(result.DeletedMappings)
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}
