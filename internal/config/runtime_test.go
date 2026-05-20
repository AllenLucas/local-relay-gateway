package config_test

import (
	"testing"

	"relay-gateway/internal/config"
)

func TestLoadUsesSafeLocalDefaults(t *testing.T) {
	t.Setenv("LRG_LISTEN_ADDR", "")
	t.Setenv("LRG_DB_PATH", "")
	t.Setenv("LRG_LOCAL_API_KEY", "")

	cfg := config.Load()

	if cfg.ListenAddr != "127.0.0.1:8787" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:8787")
	}
	if cfg.DBPath != "local-relay-gateway.db" {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, "local-relay-gateway.db")
	}
	if cfg.LocalAPIKey != "change-me-local-key" {
		t.Fatalf("LocalAPIKey = %q, want %q", cfg.LocalAPIKey, "change-me-local-key")
	}
}
