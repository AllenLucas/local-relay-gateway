package admin

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"relay-gateway/internal/core"
)

//go:embed templates/*.gohtml assets/*.js
var assets embed.FS

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
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
	store      store
	writeToken string
	staticRoot http.Handler
}

func NewHandler(store store, writeToken string) (http.Handler, error) {
	staticFS, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}

	handler := &Handler{
		store:      store,
		writeToken: writeToken,
		staticRoot: http.StripPrefix("/admin/assets/", http.FileServer(http.FS(staticFS))),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", handler.handleRoot)
	mux.HandleFunc("/admin/", handler.handleRoot)
	mux.HandleFunc("/admin/stations", handler.handleStations)
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
		openAIBaseURL, err := parseRequiredText(r.Form.Get("openai_base_url"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		openAIAPIKey, err := parseRequiredText(r.Form.Get("openai_api_key"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		anthropicBaseURL, err := parseRequiredText(r.Form.Get("anthropic_base_url"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		anthropicAPIKey, err := parseRequiredText(r.Form.Get("anthropic_api_key"))
		if err != nil {
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
		Title:      "Stations",
		WriteToken: h.writeToken,
		Stations:   stations,
	}
	if err := h.renderPage(w, "templates/stations.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
