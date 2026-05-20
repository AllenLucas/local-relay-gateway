package config

import "os"

type Runtime struct {
	ListenAddr  string
	DBPath      string
	LocalAPIKey string
}

func Load() Runtime {
	return Runtime{
		ListenAddr:  envOrDefault("LRG_LISTEN_ADDR", "127.0.0.1:8787"),
		DBPath:      envOrDefault("LRG_DB_PATH", "local-relay-gateway.db"),
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
