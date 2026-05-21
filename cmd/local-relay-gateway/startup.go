package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"relay-gateway/internal/config"
)

type browserCommand struct {
	name string
	args []string
}

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

	commands, err := browserCommands(target, os.Getenv("SystemRoot"))
	if err != nil {
		return err
	}

	var errs []error
	for _, command := range commands {
		if err := exec.Command(command.name, command.args...).Start(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", command.name, err))
			continue
		}
		return nil
	}

	return fmt.Errorf("open browser failed: %w", errors.Join(errs...))
}

func browserCommands(target string, systemRoot string) ([]browserCommand, error) {
	if target == "" {
		return nil, fmt.Errorf("browser target is empty")
	}

	commands := make([]browserCommand, 0, 3)
	if systemRoot != "" {
		rundll32 := filepath.Join(systemRoot, "System32", "rundll32.exe")
		if _, err := os.Stat(rundll32); err == nil {
			commands = append(commands, browserCommand{
				name: rundll32,
				args: []string{"url.dll,FileProtocolHandler", target},
			})
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("check Windows browser launcher: %w", err)
		}
	}

	commands = append(commands,
		browserCommand{
			name: "rundll32",
			args: []string{"url.dll,FileProtocolHandler", target},
		},
		browserCommand{
			name: "cmd",
			args: []string{"/c", "start", "", target},
		},
	)
	return commands, nil
}
