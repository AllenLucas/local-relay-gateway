package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"relay-gateway/internal/config"
)

func TestLoadWindowsBootstrapReturnsSetupModeWhenConfigMissing(t *testing.T) {
	root := t.TempDir()

	bootstrap, err := config.LoadWindowsBootstrap(root)
	if err != nil {
		t.Fatalf("LoadWindowsBootstrap error = %v", err)
	}

	if bootstrap.Mode != config.StartupModeSetup {
		t.Fatalf("Mode = %q, want %q", bootstrap.Mode, config.StartupModeSetup)
	}
	if bootstrap.Runtime.ListenAddr != config.DefaultListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", bootstrap.Runtime.ListenAddr, config.DefaultListenAddr)
	}
	if bootstrap.Runtime.DBPath != filepath.Join(root, config.DefaultDBFileName) {
		t.Fatalf("DBPath = %q", bootstrap.Runtime.DBPath)
	}
	if bootstrap.AdminWriteToken == "" {
		t.Fatal("AdminWriteToken was empty")
	}
}

func TestLoadWindowsBootstrapReturnsNormalModeWhenRuntimeFileExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, config.DefaultRuntimeFileName)
	if err := config.SaveRuntimeFile(path, config.RuntimeFile{LocalAPIKey: "local-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile error = %v", err)
	}

	bootstrap, err := config.LoadWindowsBootstrap(root)
	if err != nil {
		t.Fatalf("LoadWindowsBootstrap error = %v", err)
	}

	if bootstrap.Mode != config.StartupModeNormal {
		t.Fatalf("Mode = %q, want %q", bootstrap.Mode, config.StartupModeNormal)
	}
	if bootstrap.Runtime.LocalAPIKey != "local-key" {
		t.Fatalf("LocalAPIKey = %q, want %q", bootstrap.Runtime.LocalAPIKey, "local-key")
	}
}

func TestLoadWindowsBootstrapFallsBackToSetupModeOnInvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, config.DefaultRuntimeFileName)
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	bootstrap, err := config.LoadWindowsBootstrap(root)
	if err != nil {
		t.Fatalf("LoadWindowsBootstrap error = %v", err)
	}

	if bootstrap.Mode != config.StartupModeSetup {
		t.Fatalf("Mode = %q, want %q", bootstrap.Mode, config.StartupModeSetup)
	}
	if bootstrap.Warning == "" {
		t.Fatal("Warning was empty")
	}
}

func TestSaveRuntimeFilePersistsReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.DefaultRuntimeFileName)

	if err := config.SaveRuntimeFile(path, config.RuntimeFile{LocalAPIKey: "local-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile error = %v", err)
	}

	saved, err := config.LoadRuntimeFile(path)
	if err != nil {
		t.Fatalf("LoadRuntimeFile error = %v", err)
	}
	if saved.LocalAPIKey != "local-key" {
		t.Fatalf("LocalAPIKey = %q, want %q", saved.LocalAPIKey, "local-key")
	}
}
