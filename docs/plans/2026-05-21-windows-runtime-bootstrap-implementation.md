# Windows Runtime Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Windows double-click startup flow with first-run runtime key setup, persisted AppData config, and local runtime management pages without duplicating upstream station management.

**Architecture:** Keep the existing SQLite-backed station and mapping model unchanged, and add a separate file-backed bootstrap layer for the local gateway runtime key. Windows startup resolves runtime state before building the HTTP server, while the admin surface adds `/admin/setup` and `/admin/runtime` to manage the same `runtime.json` file and block relay traffic until setup is complete.

**Tech Stack:** Go 1.24, `net/http`, HTML templates, vanilla JS, SQLite via `modernc.org/sqlite`, Windows browser launch via standard process execution.

---

## File Structure

- Modify: `internal/config/runtime.go`
  - Keep environment-driven defaults explicit and reusable.
- Create: `internal/config/runtime_bootstrap.go`
  - Add runtime file schema, Windows AppData bootstrap resolution, setup/normal mode detection, and JSON read/write helpers.
- Create: `internal/config/runtime_bootstrap_test.go`
  - Cover missing config, valid config, invalid config, and save/load behavior.
- Modify: `internal/gateway/server.go`
  - Add setup-mode relay blocking and explicit admin handler options wiring.
- Modify: `internal/gateway/server_test.go`
  - Add setup-mode relay rejection coverage.
- Modify: `internal/admin/viewmodels.go`
  - Add setup/runtime page models and runtime notices.
- Modify: `internal/admin/handlers.go`
  - Add `/admin/setup` and `/admin/runtime`, runtime JSON persistence, and restart-required messaging.
- Create: `internal/admin/templates/setup.gohtml`
  - First-run `Local API Key` form only.
- Create: `internal/admin/templates/runtime.gohtml`
  - Local endpoint copy UI, masked key UI, and restart-required notice.
- Modify: `internal/admin/templates/layout.gohtml`
  - Add `Runtime` navigation link and keep existing page structure.
- Modify: `internal/admin/assets/admin.js`
  - Add clipboard copy and show/hide key behavior.
- Modify: `internal/admin/handlers_test.go`
  - Cover setup/runtime routes, redirects, and runtime JSON writes.
- Modify: `cmd/local-relay-gateway/main.go`
  - Keep process entrypoint small and call explicit startup orchestration.
- Create: `cmd/local-relay-gateway/startup.go`
  - Choose env-vs-Windows bootstrap mode, compute browser target, open browser, and wire listener-ready startup behavior.
- Create: `cmd/local-relay-gateway/startup_test.go`
  - Cover mode selection and browser target behavior.
- Modify: `cmd/local-relay-gateway/main_test.go`
  - Keep graceful shutdown covered after listener-based serving changes.
- Modify: `README.md`
  - Document Windows double-click startup and `AppData` runtime storage.
- Modify: `docs/usage.md`
  - Add first-run setup flow and runtime page behavior.
- Modify: `AGENTS.md`
  - Document runtime source-of-truth split if the implementation changes what future agents must read first.

### Task 1: Runtime Bootstrap Core

**Files:**
- Modify: `internal/config/runtime.go`
- Create: `internal/config/runtime_bootstrap.go`
- Create: `internal/config/runtime_bootstrap_test.go`
- Test: `internal/config/runtime_test.go`

- [ ] **Step 1: Write the failing bootstrap tests**

Add focused bootstrap tests in `internal/config/runtime_bootstrap_test.go`:

```go
package config_test

import (
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
	if bootstrap.Runtime.DBPath != filepath.Join(root, "local-relay-gateway.db") {
		t.Fatalf("DBPath = %q", bootstrap.Runtime.DBPath)
	}
	if bootstrap.AdminWriteToken == "" {
		t.Fatal("AdminWriteToken was empty")
	}
}

func TestLoadWindowsBootstrapReturnsNormalModeWhenRuntimeFileExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.json")
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
	path := filepath.Join(root, "runtime.json")
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
	path := filepath.Join(t.TempDir(), "runtime.json")

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
```

- [ ] **Step 2: Run the config tests to verify they fail**

Run:

```powershell
rtk go test ./internal/config -run "TestLoadWindowsBootstrapReturnsSetupModeWhenConfigMissing|TestLoadWindowsBootstrapReturnsNormalModeWhenRuntimeFileExists|TestLoadWindowsBootstrapFallsBackToSetupModeOnInvalidJSON|TestSaveRuntimeFilePersistsReadableJSON" -v
```

Expected:

- build failure because `LoadWindowsBootstrap`, `RuntimeFile`, `SaveRuntimeFile`, `LoadRuntimeFile`, `StartupModeSetup`, and `StartupModeNormal` do not exist yet

- [ ] **Step 3: Implement bootstrap types and file persistence**

Add the bootstrap model in `internal/config/runtime_bootstrap.go` and shared defaults in `internal/config/runtime.go`:

```go
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultListenAddr     = "127.0.0.1:8787"
	DefaultDBFileName     = "local-relay-gateway.db"
	DefaultRuntimeFileName = "runtime.json"
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
	Runtime        Runtime
	Mode           StartupMode
	RuntimeDir     string
	RuntimeFilePath string
	Warning        string
	AdminWriteToken string
}

func LoadWindowsBootstrap(root string) (Bootstrap, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Bootstrap{}, err
	}

	runtimePath := filepath.Join(root, DefaultRuntimeFileName)
	dbPath := filepath.Join(root, DefaultDBFileName)
	runtime := Runtime{
		ListenAddr: DefaultListenAddr,
		DBPath:     dbPath,
	}

	file, err := LoadRuntimeFile(runtimePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Bootstrap{
				Runtime:         runtime,
				Mode:            StartupModeSetup,
				RuntimeDir:      root,
				RuntimeFilePath: runtimePath,
				AdminWriteToken: deriveSetupWriteToken(),
			}, nil
		}

		return Bootstrap{
			Runtime:         runtime,
			Mode:            StartupModeSetup,
			RuntimeDir:      root,
			RuntimeFilePath: runtimePath,
			Warning:         "runtime.json is invalid and will be replaced when setup is saved",
			AdminWriteToken: deriveSetupWriteToken(),
		}, nil
	}

	runtime.LocalAPIKey = strings.TrimSpace(file.LocalAPIKey)
	if runtime.LocalAPIKey == "" {
		return Bootstrap{
			Runtime:         runtime,
			Mode:            StartupModeSetup,
			RuntimeDir:      root,
			RuntimeFilePath: runtimePath,
			Warning:         "runtime.json did not contain local_api_key and will be replaced when setup is saved",
			AdminWriteToken: deriveSetupWriteToken(),
		}, nil
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
		return RuntimeFile{}, err
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

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
```

Keep `Load()` environment-driven in `internal/config/runtime.go` and reuse the new defaults there:

```go
func Load() Runtime {
	return Runtime{
		ListenAddr:  envOrDefault("LRG_LISTEN_ADDR", DefaultListenAddr),
		DBPath:      envOrDefault("LRG_DB_PATH", DefaultDBFileName),
		LocalAPIKey: envOrDefault("LRG_LOCAL_API_KEY", "change-me-local-key"),
	}
}
```

- [ ] **Step 4: Run the config tests to verify they pass**

Run:

```powershell
rtk go test ./internal/config -v
```

Expected:

- all `internal/config` tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/config/runtime.go internal/config/runtime_bootstrap.go internal/config/runtime_bootstrap_test.go
git commit -m "feat: add windows runtime bootstrap config"
```

### Task 2: Setup Mode Relay Guard And Admin Runtime Pages

**Files:**
- Modify: `internal/gateway/server.go`
- Modify: `internal/gateway/server_test.go`
- Modify: `internal/admin/handlers.go`
- Modify: `internal/admin/viewmodels.go`
- Create: `internal/admin/templates/setup.gohtml`
- Create: `internal/admin/templates/runtime.gohtml`
- Modify: `internal/admin/templates/layout.gohtml`
- Modify: `internal/admin/assets/admin.js`
- Modify: `internal/admin/handlers_test.go`

- [ ] **Step 1: Write the failing gateway and admin tests**

Add relay-block and runtime-page coverage.

In `internal/gateway/server_test.go`:

```go
func TestResponsesHandlerRejectsRelayTrafficDuringSetupMode(t *testing.T) {
	handler := gateway.NewServer(gateway.Options{
		Runtime:         config.Runtime{ListenAddr: config.DefaultListenAddr},
		AdminWriteToken: "setup-write-token",
		SetupMode:       true,
	}, newGatewayTestStore(t), routing.NewSelector(nil))

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "setup") {
		t.Fatalf("body = %q, want setup guidance", recorder.Body.String())
	}
}
```

In `internal/admin/handlers_test.go`:

```go
func TestSetupPageSavesRuntimeConfigAndRedirectsToStations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	runtimePath := filepath.Join(t.TempDir(), "runtime.json")
	handler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      config.DefaultListenAddr,
		RuntimeFilePath: runtimePath,
		SetupMode:       true,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions error = %v", err)
	}

	form := url.Values{
		"write_token":   []string{adminWriteToken},
		"local_api_key": []string{"local-key"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/stations" {
		t.Fatalf("Location = %q, want %q", got, "/admin/stations")
	}

	saved, err := config.LoadRuntimeFile(runtimePath)
	if err != nil {
		t.Fatalf("LoadRuntimeFile error = %v", err)
	}
	if saved.LocalAPIKey != "local-key" {
		t.Fatalf("LocalAPIKey = %q, want %q", saved.LocalAPIKey, "local-key")
	}
}

func TestRuntimePageShowsCopyableEndpointsAndRestartNotice(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	runtimePath := filepath.Join(t.TempDir(), "runtime.json")
	if err := config.SaveRuntimeFile(runtimePath, config.RuntimeFile{LocalAPIKey: "local-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile error = %v", err)
	}

	handler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      config.DefaultListenAddr,
		RuntimeFilePath: runtimePath,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/runtime", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "http://127.0.0.1:8787/openai/v1") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Restart required") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}
```

- [ ] **Step 2: Run the gateway/admin tests to verify they fail**

Run:

```powershell
rtk go test ./internal/gateway ./internal/admin -run "TestResponsesHandlerRejectsRelayTrafficDuringSetupMode|TestSetupPageSavesRuntimeConfigAndRedirectsToStations|TestRuntimePageShowsCopyableEndpointsAndRestartNotice" -v
```

Expected:

- build failure because `gateway.Options`, `admin.Options`, `NewHandlerWithOptions`, `/admin/setup`, and `/admin/runtime` do not exist yet

- [ ] **Step 3: Implement setup-mode server options and admin pages**

Add explicit server options in `internal/gateway/server.go`:

```go
type Options struct {
	Runtime         config.Runtime
	AdminWriteToken string
	SetupMode       bool
	RuntimeFilePath string
	RuntimeWarning  string
}

type Server struct {
	cfg            config.Runtime
	adminWriteToken string
	setupMode      bool
	store          store
	selector       *routing.Selector
	client         *http.Client
}

func NewServer(options Options, store store, selector *routing.Selector) http.Handler {
	server := &Server{
		cfg:             options.Runtime,
		adminWriteToken: options.AdminWriteToken,
		setupMode:       options.SetupMode,
		store:           store,
		selector:        selector,
		client:          &http.Client{Transport: cloneDefaultTransport()},
	}

	adminHandler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      options.AdminWriteToken,
		ListenAddr:      options.Runtime.ListenAddr,
		RuntimeFilePath: options.RuntimeFilePath,
		RuntimeWarning:  options.RuntimeWarning,
		SetupMode:       options.SetupMode,
	})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/openai/v1/responses", server.handleResponses)
	mux.HandleFunc("/openai/v1/chat/completions", server.handleChatCompletions)
	mux.HandleFunc("/anthropic/v1/messages", server.handleAnthropicMessages)
	mux.Handle("/admin", adminHandler)
	mux.Handle("/admin/", adminHandler)
	return mux
}

func (s *Server) handleNormalizedRequest(
	w http.ResponseWriter,
	r *http.Request,
	protocol core.Protocol,
	normalize func(*http.Request) (core.NormalizedRequest, error),
	build requestBuilder,
) {
	if s.setupMode {
		http.Error(w, "gateway setup is not complete; open /admin/setup and restart after saving Local API Key", http.StatusServiceUnavailable)
		return
	}
	if !s.authorize(r, protocol) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// existing normalize and proxy flow continues here
}
```

Add `admin.Options`, setup/runtime handlers, and runtime-page models in `internal/admin/handlers.go` and `internal/admin/viewmodels.go`:

```go
type Options struct {
	WriteToken      string
	ListenAddr      string
	RuntimeFilePath string
	RuntimeWarning  string
	SetupMode       bool
}

type SetupPage struct {
	Title         string
	WriteToken    string
	RuntimeWarning string
}

type RuntimePage struct {
	Title              string
	WriteToken         string
	LocalAPIKey        string
	OpenAIBaseURL      string
	AnthropicBaseURL   string
	RestartRequired    bool
	RuntimeFilePath    string
}

func NewHandlerWithOptions(store store, options Options) (http.Handler, error) {
	// existing template/static setup
	mux.HandleFunc("/admin/setup", handler.handleSetup)
	mux.HandleFunc("/admin/runtime", handler.handleRuntime)
	return mux, nil
}

func NewHandler(store store, writeToken string) (http.Handler, error) {
	return NewHandlerWithOptions(store, Options{
		WriteToken: writeToken,
		ListenAddr: config.DefaultListenAddr,
	})
}

func (h *Handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		localAPIKey, err := parseRequiredText(r.Form.Get("local_api_key"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.SaveRuntimeFile(h.runtimeFilePath, config.RuntimeFile{LocalAPIKey: localAPIKey}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/stations", http.StatusSeeOther)
		return
	}

	if _, err := config.LoadRuntimeFile(h.runtimeFilePath); err == nil {
		http.Redirect(w, r, "/admin/runtime", http.StatusSeeOther)
		return
	}

	data := SetupPage{
		Title:          "Setup",
		WriteToken:     h.writeToken,
		RuntimeWarning: h.runtimeWarning,
	}
	if err := h.renderPage(w, "templates/setup.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !h.isValidWriteToken(r.Form.Get("write_token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		localAPIKey, err := parseRequiredText(r.Form.Get("local_api_key"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.SaveRuntimeFile(h.runtimeFilePath, config.RuntimeFile{LocalAPIKey: localAPIKey}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/runtime?saved=1", http.StatusSeeOther)
		return
	}

	file, _ := config.LoadRuntimeFile(h.runtimeFilePath)
	data := RuntimePage{
		Title:            "Runtime",
		WriteToken:       h.writeToken,
		LocalAPIKey:      file.LocalAPIKey,
		OpenAIBaseURL:    "http://" + h.listenAddr + "/openai/v1",
		AnthropicBaseURL: "http://" + h.listenAddr + "/anthropic",
		RestartRequired:  r.URL.Query().Get("saved") == "1",
		RuntimeFilePath:  h.runtimeFilePath,
	}
	if err := h.renderPage(w, "templates/runtime.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Add minimal template and JS affordances:

```html
<!-- internal/admin/templates/setup.gohtml -->
{{define "content"}}
<h1>Setup</h1>
{{if .RuntimeWarning}}<p>{{.RuntimeWarning}}</p>{{end}}
<form method="post" action="/admin/setup">
  <input type="hidden" name="write_token" value="{{.WriteToken}}">
  <input name="local_api_key" placeholder="Local API Key" required>
  <button type="submit">Save Local API Key</button>
</form>
{{end}}
```

```html
<!-- internal/admin/templates/runtime.gohtml -->
{{define "content"}}
<h1>Runtime</h1>
{{if .RestartRequired}}<p>Restart required: the saved Local API Key takes effect after the process is restarted.</p>{{end}}
<p>OpenAI Base URL: <code id="runtime-openai-url">{{.OpenAIBaseURL}}</code> <button type="button" data-copy-target="runtime-openai-url">Copy</button></p>
<p>Anthropic Base URL: <code id="runtime-anthropic-url">{{.AnthropicBaseURL}}</code> <button type="button" data-copy-target="runtime-anthropic-url">Copy</button></p>
<form method="post" action="/admin/runtime">
  <input type="hidden" name="write_token" value="{{.WriteToken}}">
  <input id="runtime-local-key" name="local_api_key" type="password" value="{{.LocalAPIKey}}" required>
  <button type="button" data-toggle-password="runtime-local-key">Show / Hide</button>
  <button type="button" data-copy-input="runtime-local-key">Copy Key</button>
  <button type="submit">Save Runtime Key</button>
</form>
{{end}}
```

```javascript
document.addEventListener("DOMContentLoaded", () => {
  const active = document.querySelector(`a[href="${window.location.pathname}"]`);
  if (active) active.style.fontWeight = "700";

  document.querySelectorAll("[data-copy-target]").forEach((button) => {
    button.addEventListener("click", async () => {
      const target = document.getElementById(button.dataset.copyTarget);
      if (!target) return;
      await navigator.clipboard.writeText(target.textContent.trim());
    });
  });

  document.querySelectorAll("[data-copy-input]").forEach((button) => {
    button.addEventListener("click", async () => {
      const target = document.getElementById(button.dataset.copyInput);
      if (!target) return;
      await navigator.clipboard.writeText(target.value);
    });
  });

  document.querySelectorAll("[data-toggle-password]").forEach((button) => {
    button.addEventListener("click", () => {
      const target = document.getElementById(button.dataset.togglePassword);
      if (!target) return;
      target.type = target.type === "password" ? "text" : "password";
    });
  });
});
```

- [ ] **Step 4: Run the gateway/admin tests to verify they pass**

Run:

```powershell
rtk go test ./internal/gateway ./internal/admin -v
```

Expected:

- all `internal/gateway` and `internal/admin` tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/server.go internal/gateway/server_test.go internal/admin/handlers.go internal/admin/viewmodels.go internal/admin/templates/setup.gohtml internal/admin/templates/runtime.gohtml internal/admin/templates/layout.gohtml internal/admin/assets/admin.js internal/admin/handlers_test.go
git commit -m "feat: add runtime setup and management pages"
```

### Task 3: Windows Startup Orchestration

**Files:**
- Modify: `cmd/local-relay-gateway/main.go`
- Modify: `cmd/local-relay-gateway/main_test.go`
- Create: `cmd/local-relay-gateway/startup.go`
- Create: `cmd/local-relay-gateway/startup_test.go`

- [ ] **Step 1: Write the failing startup tests**

Create `cmd/local-relay-gateway/startup_test.go`:

```go
package main

import (
	"testing"

	"relay-gateway/internal/config"
)

func TestSelectStartupModeUsesWindowsBootstrapWhenNoRuntimeEnvExists(t *testing.T) {
	mode := selectStartupMode("windows", false)
	if mode != config.StartupModeSetup {
		t.Fatalf("mode = %q, want initial Windows bootstrap mode", mode)
	}
}

func TestBrowserTargetUsesSetupRouteForSetupMode(t *testing.T) {
	got := browserTarget(config.StartupModeSetup, "127.0.0.1:8787")
	want := "http://127.0.0.1:8787/admin/setup"
	if got != want {
		t.Fatalf("browserTarget = %q, want %q", got, want)
	}
}

func TestBrowserTargetUsesStationsRouteForNormalMode(t *testing.T) {
	got := browserTarget(config.StartupModeNormal, "127.0.0.1:8787")
	want := "http://127.0.0.1:8787/admin/stations"
	if got != want {
		t.Fatalf("browserTarget = %q, want %q", got, want)
	}
}
```

Extend `cmd/local-relay-gateway/main_test.go` with listener-based serving coverage:

```go
func TestServeHTTPInvokesOnReadyBeforeShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error = %v", err)
	}

	var ready atomic.Bool
	server := &http.Server{Handler: newRootHandler(http.NotFoundHandler())}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(ctx, server, listener, func() { ready.Store(true) }, nil)
	}()

	time.Sleep(100 * time.Millisecond)
	if !ready.Load() {
		t.Fatal("onReady callback was not invoked")
	}
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("serveHTTP error = %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run the startup tests to verify they fail**

Run:

```powershell
rtk go test ./cmd/local-relay-gateway -run "TestSelectStartupModeUsesWindowsBootstrapWhenNoRuntimeEnvExists|TestBrowserTargetUsesSetupRouteForSetupMode|TestBrowserTargetUsesStationsRouteForNormalMode|TestServeHTTPInvokesOnReadyBeforeShutdown" -v
```

Expected:

- build failure because `selectStartupMode`, `browserTarget`, and the new `serveHTTP` signature do not exist yet

- [ ] **Step 3: Implement explicit Windows startup orchestration**

Create `cmd/local-relay-gateway/startup.go`:

```go
package main

import (
	"net"
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

func loadBootstrap() (config.Bootstrap, bool, error) {
	if config.HasRuntimeEnv() {
		runtimeCfg := config.Load()
		return config.Bootstrap{
			Runtime:         runtimeCfg,
			Mode:            config.StartupModeEnv,
			AdminWriteToken: deriveAdminWriteToken(runtimeCfg.LocalAPIKey),
		}, false, nil
	}

	if runtime.GOOS == "windows" {
		root := filepath.Join(os.Getenv("LOCALAPPDATA"), "LocalRelayGateway")
		bootstrap, err := config.LoadWindowsBootstrap(root)
		return bootstrap, true, err
	}

	runtimeCfg := config.Load()
	return config.Bootstrap{
		Runtime:         runtimeCfg,
		Mode:            config.StartupModeEnv,
		AdminWriteToken: deriveAdminWriteToken(runtimeCfg.LocalAPIKey),
	}, false, nil
}

func browserTarget(mode config.StartupMode, listenAddr string) string {
	if mode == config.StartupModeSetup {
		return "http://" + listenAddr + "/admin/setup"
	}
	return "http://" + listenAddr + "/admin/stations"
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func listenerAddress(listener net.Listener) string {
	return listener.Addr().String()
}
```

Adjust `cmd/local-relay-gateway/main.go` to use explicit bootstrap + ready callback:

```go
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

	server := &http.Server{
		Addr: bootstrap.Runtime.ListenAddr,
		Handler: newRootHandler(gateway.NewServer(gateway.Options{
			Runtime:         bootstrap.Runtime,
			AdminWriteToken: bootstrap.AdminWriteToken,
			SetupMode:       bootstrap.Mode == config.StartupModeSetup,
			RuntimeFilePath: bootstrap.RuntimeFilePath,
			RuntimeWarning:  bootstrap.Warning,
		}, store, selector)),
	}

	listener, err := net.Listen("tcp", bootstrap.Runtime.ListenAddr)
	if err != nil {
		log.Fatal(err)
	}

	target := browserTarget(bootstrap.Mode, bootstrap.Runtime.ListenAddr)
	log.Printf("local relay gateway listening on %s", bootstrap.Runtime.ListenAddr)
	if err := serveHTTP(rootCtx, server, listener, func() {
		if autoOpenBrowser {
			if err := openBrowser(target); err != nil {
				log.Printf("open browser failed: %v; visit %s", err, target)
			}
		}
	}, func() {
		log.Printf("local relay gateway shutting down")
	}); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, onReady func(), onShutdown func()) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	if onReady != nil {
		onReady()
	}
	// existing shutdown select logic continues here
}
```

- [ ] **Step 4: Run the startup tests to verify they pass**

Run:

```powershell
rtk go test ./cmd/local-relay-gateway -v
```

Expected:

- all `cmd/local-relay-gateway` tests pass

- [ ] **Step 5: Commit**

```bash
git add cmd/local-relay-gateway/main.go cmd/local-relay-gateway/main_test.go cmd/local-relay-gateway/startup.go cmd/local-relay-gateway/startup_test.go
git commit -m "feat: add windows bootstrap startup flow"
```

### Task 4: Documentation And Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/usage.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Write the failing documentation checklist**

Before editing docs, create a quick checklist and mark anything currently missing:

```text
[ ] README explains Windows double-click startup
[ ] README explains first-run /admin/setup behavior
[ ] README explains runtime.json path in %LocalAppData%\LocalRelayGateway\
[ ] README explains Windows database path in %LocalAppData%\LocalRelayGateway\
[ ] docs/usage.md explains restart-required after Local API Key changes
[ ] AGENTS.md explains env-mode vs Windows file-bootstrap source of truth
```

- [ ] **Step 2: Update the docs**

Apply the documented runtime flow in prose similar to:

```md
## Windows Double-Click Startup

On Windows, the formal executable can be started by double-clicking `local-relay-gateway.exe`.

- First run opens `http://127.0.0.1:8787/admin/setup`
- The runtime key is stored at `%LocalAppData%\LocalRelayGateway\runtime.json`
- The default database file is `%LocalAppData%\LocalRelayGateway\local-relay-gateway.db`
- After saving a new Local API Key from `/admin/runtime`, restart the process once for the new key to take effect
```

Update `AGENTS.md` so future agents know:

```md
- Environment variables remain the developer override path
- Windows no-env startup uses `%LocalAppData%\LocalRelayGateway\runtime.json`
- `/admin/runtime` manages local gateway identity only
- `/admin/stations` remains the only upstream station configuration source
```

- [ ] **Step 3: Run the full verification suite**

Run:

```powershell
rtk go test ./... -v
```

Expected:

- all repository tests pass

- [ ] **Step 4: Commit**

```bash
git add README.md docs/usage.md AGENTS.md
git commit -m "docs: describe windows bootstrap workflow"
```

## Self-Review

### Spec Coverage

- Windows double-click startup: covered in Task 3
- First-run `/admin/setup`: covered in Task 2
- Persisted `runtime.json`: covered in Task 1 and Task 2
- `AppData` database default: covered in Task 1 and Task 4
- Copyable local endpoints and runtime key management: covered in Task 2
- Setup-mode relay block: covered in Task 2
- Restart-required runtime key updates: covered in Task 2 and Task 4
- Graceful shutdown preservation: covered in Task 3 and the final test run

### Placeholder Scan

- No `TODO` or `TBD` markers remain
- Every task lists concrete files
- Every test step has explicit function names and commands
- Every implementation step names the target types and handlers to add

### Type Consistency

- `config.Bootstrap`, `config.RuntimeFile`, and `config.StartupMode*` are used consistently across config, gateway, admin, and startup tasks
- `gateway.Options` is the single server-construction type used in later tasks
- `admin.Options` and `NewHandlerWithOptions` are the only new admin constructor surface used in later tasks

