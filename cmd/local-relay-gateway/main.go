package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"relay-gateway/internal/config"
	"relay-gateway/internal/gateway"
	"relay-gateway/internal/jobs"
	"relay-gateway/internal/routing"
	sqlitestore "relay-gateway/internal/storage/sqlite"
)

func main() {
	cfg := config.Load()
	store, err := sqlitestore.NewStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	selector := routing.NewSelector(nil)
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	jobs.StartHealthLoop(rootCtx, store, selector)
	jobs.StartRetentionLoop(rootCtx, store, 7*24*time.Hour, time.Hour)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: newRootHandler(gateway.NewServer(cfg, store, selector)),
	}

	log.Printf("local relay gateway listening on %s", cfg.ListenAddr)
	if err := serveHTTP(rootCtx, server, func() {
		log.Printf("local relay gateway shutting down")
	}); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(ctx context.Context, server *http.Server, onShutdown func()) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if onShutdown != nil {
			onShutdown()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newRootHandler(gatewayHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", gatewayHandler)
	return mux
}
