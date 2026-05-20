package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"relay-gateway/internal/core"
	"relay-gateway/internal/routing"
)

type healthStore interface {
	ListStations(ctx context.Context) ([]core.Station, error)
	ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error)
	SaveStationStatus(ctx context.Context, status core.StationStatus) error
}

func StartHealthLoop(ctx context.Context, store healthStore, selector *routing.Selector) {
	if store == nil || selector == nil {
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(15 * time.Second)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stations, err := store.ListStations(ctx)
				if err != nil {
					continue
				}
				statuses, err := store.ListStationStatuses(ctx)
				if err != nil {
					continue
				}

				for _, station := range stations {
					status := statuses[station.ID]
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL(station.OpenAIBaseURL), nil)
					if err != nil {
						continue
					}

					resp, err := client.Do(req)
					if healthCheckFailed(err, resp) {
						message := "health check failed"
						if err != nil {
							message = err.Error()
						} else {
							message = resp.Status
						}
						status = selector.RecordFailure(station, status, message)
					} else {
						status = selector.RecordSuccess(station, status)
					}

					if resp != nil && resp.Body != nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
					}
					_ = store.SaveStationStatus(ctx, status)
				}
			}
		}
	}()
}

func healthURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		return "/models"
	}
	return trimmed + "/models"
}

func healthCheckFailed(err error, resp *http.Response) bool {
	return err != nil || (resp != nil && resp.StatusCode >= http.StatusInternalServerError)
}
