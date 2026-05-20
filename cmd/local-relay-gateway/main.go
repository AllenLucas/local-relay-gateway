package main

import (
	"context"
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

	log.Printf("local relay gateway listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, newRootHandler(gateway.NewServer(cfg, store, selector))); err != nil {
		log.Fatal(err)
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
