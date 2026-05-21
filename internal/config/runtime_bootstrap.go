package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StartupMode string

const (
	StartupModeEnv    StartupMode = "env"
	StartupModeSetup  StartupMode = "setup"
	StartupModeNormal StartupMode = "normal"
)

type RuntimeFile struct {
	LocalAPIKey string `json:"local_api_key"`
}

type Bootstrap struct {
	Runtime         Runtime
	Mode            StartupMode
	RuntimeDir      string
	RuntimeFilePath string
	Warning         string
	AdminWriteToken string
}

func LoadWindowsBootstrap(root string) (Bootstrap, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Bootstrap{}, err
	}

	runtimePath := filepath.Join(root, DefaultRuntimeFileName)
	runtime := Runtime{
		ListenAddr: DefaultListenAddr,
		DBPath:     filepath.Join(root, DefaultDBFileName),
	}

	file, err := LoadRuntimeFile(runtimePath)
	if err != nil {
		bootstrap := setupBootstrap(root, runtimePath, runtime)
		if errors.Is(err, os.ErrNotExist) {
			return bootstrap, nil
		}
		if errors.Is(err, errInvalidRuntimeFile) {
			bootstrap.Warning = "runtime.json is invalid and will be replaced when setup is saved"
			return bootstrap, nil
		}
		return Bootstrap{}, err
	}

	runtime.LocalAPIKey = strings.TrimSpace(file.LocalAPIKey)
	if runtime.LocalAPIKey == "" {
		bootstrap := setupBootstrap(root, runtimePath, runtime)
		bootstrap.Warning = "runtime.json did not contain local_api_key and will be replaced when setup is saved"
		return bootstrap, nil
	}

	return Bootstrap{
		Runtime:         runtime,
		Mode:            StartupModeNormal,
		RuntimeDir:      root,
		RuntimeFilePath: runtimePath,
		AdminWriteToken: deriveAdminWriteToken(runtime.LocalAPIKey),
	}, nil
}

func LoadRuntimeFile(path string) (RuntimeFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return RuntimeFile{}, err
	}

	var file RuntimeFile
	if err := json.Unmarshal(body, &file); err != nil {
		return RuntimeFile{}, fmt.Errorf("%w: %v", errInvalidRuntimeFile, err)
	}
	file.LocalAPIKey = strings.TrimSpace(file.LocalAPIKey)
	return file, nil
}

func SaveRuntimeFile(path string, file RuntimeFile) error {
	file.LocalAPIKey = strings.TrimSpace(file.LocalAPIKey)
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func setupBootstrap(root string, runtimePath string, runtime Runtime) Bootstrap {
	return Bootstrap{
		Runtime:         runtime,
		Mode:            StartupModeSetup,
		RuntimeDir:      root,
		RuntimeFilePath: runtimePath,
		AdminWriteToken: deriveSetupWriteToken(),
	}
}

func deriveSetupWriteToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return hashToken("local-relay-gateway setup write token")
	}
	return hex.EncodeToString(raw[:])
}

func deriveAdminWriteToken(localAPIKey string) string {
	if token := strings.TrimSpace(localAPIKey); token != "" {
		return hashToken("local-relay-gateway admin write token\x00" + token)
	}
	return deriveSetupWriteToken()
}

var errInvalidRuntimeFile = errors.New("invalid runtime file")

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
