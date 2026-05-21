package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"relay-gateway/internal/config"
)

func selectStartupMode(goos string, hasRuntimeEnv bool) config.StartupMode {
	if hasRuntimeEnv {
		return config.StartupModeEnv
	}
	if goos == "windows" {
		return config.StartupModeSetup
	}
	return config.StartupModeEnv
}

func hasRuntimeEnv() bool {
	return os.Getenv("LRG_LOCAL_API_KEY") != "" ||
		os.Getenv("LRG_LISTEN_ADDR") != "" ||
		os.Getenv("LRG_DB_PATH") != ""
}

func loadBootstrap() (config.Bootstrap, bool, error) {
	return loadBootstrapFor(runtime.GOOS)
}

func loadBootstrapFor(goos string) (config.Bootstrap, bool, error) {
	mode := selectStartupMode(goos, hasRuntimeEnv())
	if mode == config.StartupModeEnv {
		return config.Bootstrap{
			Runtime: config.Load(),
			Mode:    config.StartupModeEnv,
		}, false, nil
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return config.Bootstrap{}, false, errors.New("LOCALAPPDATA is required for Windows bootstrap startup")
	}

	bootstrap, err := config.LoadWindowsBootstrap(filepath.Join(localAppData, "LocalRelayGateway"))
	if err != nil {
		return config.Bootstrap{}, false, err
	}
	return bootstrap, true, nil
}

func browserTarget(mode config.StartupMode, listenAddr string) string {
	path := "/admin/stations"
	if mode == config.StartupModeSetup {
		path = "/admin/setup"
	}

	return (&url.URL{
		Scheme: "http",
		Host:   listenAddr,
		Path:   path,
	}).String()
}

func openBrowser(target string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if target == "" {
		return fmt.Errorf("browser target is empty")
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
