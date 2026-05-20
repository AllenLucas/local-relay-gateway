package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRootHandlerServesHealthzAndDelegatesGateway(t *testing.T) {
	gatewayHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gateway", "1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("gateway"))
	})

	handler := newRootHandler(gatewayHandler)

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthResp := httptest.NewRecorder()
	handler.ServeHTTP(healthResp, healthReq)

	if healthResp.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthResp.Code, http.StatusOK)
	}
	if got := healthResp.Body.String(); got != "ok" {
		t.Fatalf("health body = %q, want %q", got, "ok")
	}

	gatewayReq := httptest.NewRequest(http.MethodGet, "/openai/v1/responses", nil)
	gatewayResp := httptest.NewRecorder()
	handler.ServeHTTP(gatewayResp, gatewayReq)

	if gatewayResp.Code != http.StatusCreated {
		t.Fatalf("gateway status = %d, want %d", gatewayResp.Code, http.StatusCreated)
	}
	if got := gatewayResp.Header().Get("X-Gateway"); got != "1" {
		t.Fatalf("gateway header = %q, want %q", got, "1")
	}
	if got := gatewayResp.Body.String(); got != "gateway" {
		t.Fatalf("gateway body = %q, want %q", got, "gateway")
	}
}
