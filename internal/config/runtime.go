package config

import "os"

const (
	DefaultListenAddr      = "127.0.0.1:8787"
	DefaultDBFileName      = "local-relay-gateway.db"
	DefaultRuntimeFileName = "runtime.json"
)

type Runtime struct {
	ListenAddr  string
	DBPath      string
	LocalAPIKey string
}

func Load() Runtime {
	return Runtime{
		ListenAddr:  envOrDefault("LRG_LISTEN_ADDR", DefaultListenAddr),
		DBPath:      envOrDefault("LRG_DB_PATH", DefaultDBFileName),
		LocalAPIKey: envOrDefault("LRG_LOCAL_API_KEY", "change-me-local-key"),
	}
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
