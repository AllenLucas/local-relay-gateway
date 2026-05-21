package main

import (
	"strings"
	"testing"

	"relay-gateway/internal/config"
)

func TestSelectStartupModeUsesSetupForWindowsWithoutRuntimeEnv(t *testing.T) {
	got := selectStartupMode("windows", false)
	if got != config.StartupModeSetup {
		t.Fatalf("selectStartupMode = %q, want %q", got, config.StartupModeSetup)
	}
}

func TestBrowserTargetUsesSetupPageInSetupMode(t *testing.T) {
	got := browserTarget(config.StartupModeSetup, "127.0.0.1:8787")
	want := "http://127.0.0.1:8787/admin/setup"
	if got != want {
		t.Fatalf("browserTarget = %q, want %q", got, want)
	}
}

func TestBrowserTargetUsesStationsPageOutsideSetupMode(t *testing.T) {
	got := browserTarget(config.StartupModeNormal, "127.0.0.1:8787")
	want := "http://127.0.0.1:8787/admin/stations"
	if got != want {
		t.Fatalf("browserTarget = %q, want %q", got, want)
	}
}

func TestLoadBootstrapReturnsClearErrorWhenWindowsLocalAppDataIsEmpty(t *testing.T) {
	t.Setenv("LRG_LOCAL_API_KEY", "")
	t.Setenv("LRG_LISTEN_ADDR", "")
	t.Setenv("LRG_DB_PATH", "")
	t.Setenv("LOCALAPPDATA", "")

	_, _, err := loadBootstrapFor("windows")
	if err == nil {
		t.Fatal("loadBootstrapFor error was nil")
	}
	if !strings.Contains(err.Error(), "LOCALAPPDATA") {
		t.Fatalf("loadBootstrapFor error = %q, want LOCALAPPDATA context", err.Error())
	}
}
