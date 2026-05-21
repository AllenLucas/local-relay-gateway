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

func TestLoadWindowsBootstrapDerivesStableAdminWriteToken(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, config.DefaultRuntimeFileName)
	if err := config.SaveRuntimeFile(path, config.RuntimeFile{LocalAPIKey: "local-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile error = %v", err)
	}

	first, err := config.LoadWindowsBootstrap(root)
	if err != nil {
		t.Fatalf("LoadWindowsBootstrap first error = %v", err)
	}
	second, err := config.LoadWindowsBootstrap(root)
	if err != nil {
		t.Fatalf("LoadWindowsBootstrap second error = %v", err)
	}

	if first.AdminWriteToken == "" {
		t.Fatal("AdminWriteToken was empty")
	}
	if first.AdminWriteToken == "local-key" {
		t.Fatal("AdminWriteToken must not expose raw LocalAPIKey")
	}
	if first.AdminWriteToken != second.AdminWriteToken {
		t.Fatalf("AdminWriteToken was not stable: %q != %q", first.AdminWriteToken, second.AdminWriteToken)
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

func TestLoadWindowsBootstrapReturnsReadErrorForRuntimeFileFilesystemFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, config.DefaultRuntimeFileName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir error = %v", err)
	}

	_, err := config.LoadWindowsBootstrap(root)
	if err == nil {
		t.Fatal("LoadWindowsBootstrap error was nil")
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

func TestSaveRuntimeFileReplacesExistingRuntimeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.DefaultRuntimeFileName)

	if err := config.SaveRuntimeFile(path, config.RuntimeFile{LocalAPIKey: "old-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile old error = %v", err)
	}
	if err := config.SaveRuntimeFile(path, config.RuntimeFile{LocalAPIKey: "new-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile new error = %v", err)
	}

	saved, err := config.LoadRuntimeFile(path)
	if err != nil {
		t.Fatalf("LoadRuntimeFile error = %v", err)
	}
	if saved.LocalAPIKey != "new-key" {
		t.Fatalf("LocalAPIKey = %q, want %q", saved.LocalAPIKey, "new-key")
	}
}
