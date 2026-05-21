package main

import (
	"context"
	"errors"
	"log"
	"net"
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
	bootstrap, autoOpenBrowser, err := loadBootstrap()
	if err != nil {
		log.Fatal(err)
	}

	store, err := sqlitestore.NewStore(bootstrap.Runtime.DBPath)
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

	gatewayHandler := gateway.NewServer(bootstrap.Runtime, store, selector)
	if bootstrap.Mode != config.StartupModeEnv {
		gatewayHandler = gateway.NewServerWithOptions(gateway.Options{
			Runtime:         bootstrap.Runtime,
			AdminWriteToken: bootstrap.AdminWriteToken,
			SetupMode:       bootstrap.Mode == config.StartupModeSetup,
			RuntimeFilePath: bootstrap.RuntimeFilePath,
			RuntimeWarning:  bootstrap.Warning,
		}, store, selector)
	}

	server := &http.Server{
		Addr:    bootstrap.Runtime.ListenAddr,
		Handler: newRootHandler(gatewayHandler),
	}
	listener, err := net.Listen("tcp", bootstrap.Runtime.ListenAddr)
	if err != nil {
		log.Fatal(err)
	}

	onReady := func() {
		log.Printf("local relay gateway listening on %s", bootstrap.Runtime.ListenAddr)
		if !autoOpenBrowser {
			return
		}
		target := browserTarget(bootstrap.Mode, bootstrap.Runtime.ListenAddr)
		if err := openBrowser(target); err != nil {
			log.Printf("open browser failed: %v; open %s manually", err, target)
		}
	}
	if err := serveHTTP(rootCtx, server, listener, onReady, func() {
		log.Printf("local relay gateway shutting down")
	}); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, onReady func(), onShutdown func()) error {
	errCh := make(chan error, 1)
	go func() {
		if onReady != nil {
			onReady()
		}
		errCh <- server.Serve(listener)
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
