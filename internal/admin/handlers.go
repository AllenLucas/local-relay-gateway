package admin

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"

	"relay-gateway/internal/core"
)

//go:embed templates/*.gohtml assets/*.js
var assets embed.FS

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
	ListStations(ctx context.Context) ([]core.Station, error)
	ListMappings(ctx context.Context) ([]core.ModelMapping, error)
	UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error
}

type Handler struct {
	store      store
	staticRoot http.Handler
}

func NewHandler(store store) (http.Handler, error) {
	staticFS, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}

	handler := &Handler{
		store:      store,
		staticRoot: http.StripPrefix("/admin/assets/", http.FileServer(http.FS(staticFS))),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", handler.handleRoot)
	mux.HandleFunc("/admin/", handler.handleRoot)
	mux.HandleFunc("/admin/stations", handler.handleStations)
	mux.HandleFunc("/admin/mappings", handler.handleMappings)
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

		priority, err := parseRequiredInt(r.Form.Get("priority"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cooldownSeconds, err := parseRequiredInt(r.Form.Get("cooldown_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		healthInterval, err := parseRequiredInt(r.Form.Get("health_check_interval_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		healthTimeout, err := parseRequiredInt(r.Form.Get("health_check_timeout_seconds"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		failureThreshold, err := parseRequiredInt(r.Form.Get("consecutive_failure_threshold"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		recoveryThreshold, err := parseRequiredInt(r.Form.Get("consecutive_recovery_threshold"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = h.store.CreateStation(r.Context(), core.Station{
			Name:                         r.Form.Get("name"),
			Enabled:                      r.Form.Get("enabled") == "on",
			Priority:                     priority,
			CooldownSeconds:              cooldownSeconds,
			HealthCheckIntervalSeconds:   healthInterval,
			HealthCheckTimeoutSeconds:    healthTimeout,
			ConsecutiveFailureThreshold:  failureThreshold,
			ConsecutiveRecoveryThreshold: recoveryThreshold,
			OpenAIBaseURL:                r.Form.Get("openai_base_url"),
			OpenAIAPIKey:                 r.Form.Get("openai_api_key"),
			AnthropicBaseURL:             r.Form.Get("anthropic_base_url"),
			AnthropicAPIKey:              r.Form.Get("anthropic_api_key"),
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
		Title:    "Stations",
		Stations: stations,
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

		stationID, err := strconv.ParseInt(r.Form.Get("station_id"), 10, 64)
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
			Protocol:      core.Protocol(r.Form.Get("protocol")),
			Alias:         r.Form.Get("alias"),
			UpstreamModel: r.Form.Get("upstream_model"),
			Enabled:       r.Form.Get("enabled") != "off",
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
		Title:    "Mappings",
		Stations: stations,
		Mappings: mappings,
	}
	if err := h.renderPage(w, "templates/mappings.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseRequiredInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("invalid numeric input")
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
