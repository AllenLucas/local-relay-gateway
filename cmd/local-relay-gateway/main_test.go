package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var errUnexpectedReadyHealthz = errors.New("unexpected healthz response during ready callback")

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

func TestServeHTTPExitsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var closed atomic.Bool
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}
	server := &http.Server{
		Handler: newRootHandler(http.NotFoundHandler()),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(ctx, server, listener, nil, func() { closed.Store(true) })
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveHTTP error = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTP did not exit after context cancellation")
	}

	if !closed.Load() {
		t.Fatal("shutdown callback was not invoked")
	}
}

func TestServeHTTPInvokesOnReadyBeforeShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}

	var ready atomic.Bool
	var shutdownSawReady atomic.Bool
	server := &http.Server{
		Handler: newRootHandler(http.NotFoundHandler()),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(ctx, server, listener, func() { ready.Store(true) }, func() {
			shutdownSawReady.Store(ready.Load())
		})
	}()

	deadline := time.After(3 * time.Second)
	for !ready.Load() {
		select {
		case <-deadline:
			t.Fatal("ready callback was not invoked")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveHTTP error = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTP did not exit after context cancellation")
	}

	if !shutdownSawReady.Load() {
		t.Fatal("shutdown callback ran before ready callback")
	}
}

func TestServeHTTPServesHealthzWhenReadyCallbackRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}
	addr := listener.Addr().String()

	server := &http.Server{Handler: newRootHandler(http.NotFoundHandler())}
	readyErr := make(chan error, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(ctx, server, listener, func() {
			resp, err := http.Get("http://" + addr + "/healthz")
			if err != nil {
				readyErr <- err
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				readyErr <- err
				return
			}
			if resp.StatusCode != http.StatusOK || string(body) != "ok" {
				readyErr <- errUnexpectedReadyHealthz
				return
			}
			readyErr <- nil
		}, nil)
	}()

	select {
	case err := <-readyErr:
		if err != nil {
			t.Fatalf("healthz during ready callback error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready callback did not complete")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveHTTP error = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTP did not exit after context cancellation")
	}
}
