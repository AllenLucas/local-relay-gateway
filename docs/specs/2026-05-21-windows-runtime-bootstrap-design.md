# Windows Runtime Bootstrap Design

## Status

- Approved in chat before implementation
- Ready for implementation planning

## Why This Spec Lives Here

This repository currently ignores `docs/superpowers/` in `.gitignore`, so a spec stored there would not be committed or available for long-term recovery on other devices.

This spec is intentionally stored in `docs/specs/` so it remains part of the repository history and is available to future AI agents and future maintenance sessions after long inactivity.

## Problem

The current gateway startup flow is correct for development, but inconvenient for normal Windows use:

- the operator must open a terminal manually
- the operator must set temporary environment variables manually
- the operator must then run the Go command manually

This is friction for a project whose main goal is to behave like a stable local tool for Codex CLI, Claude Code, and other local clients.

The project already persists upstream station configuration in SQLite, but the local runtime configuration still depends on environment variables only.

## Goals

- Support Windows double-click startup without requiring manual environment-variable entry
- Keep the existing station and mapping model unchanged
- Add a first-run web setup flow for the local gateway key
- Persist local runtime configuration across restarts
- Automatically open the browser on startup for the expected admin page
- Keep foreground console behavior and `Ctrl+C` graceful shutdown
- Keep implementation boundaries clear for future AI maintenance

## Non-Goals

- No new duplicate station-management page
- No hot reload for runtime key changes in the first version
- No DPAPI or platform-specific encryption in the first version
- No Linux or macOS browser auto-launch behavior changes in the first version
- No change to current upstream station storage format
- No change to current OpenAI or Anthropic relay protocol behavior

## Confirmed Product Decisions

- Windows formal entrypoint uses one console `.exe`, not `launcher.exe + gateway.exe`
- The console window remains visible during runtime
- Browser opens automatically on every Windows startup
- First-run setup only collects `Local API Key`
- `ListenAddr` remains `127.0.0.1:8787` in the first version
- Default SQLite path moves to the local app data directory for Windows formal startup
- Runtime config is stored as plaintext JSON
- Runtime changes save immediately but only take effect after restart
- Existing station configuration remains in `/admin/stations`

## Existing Constraints

### Current runtime config

`internal/config/runtime.go` currently loads:

- `LRG_LOCAL_API_KEY`
- `LRG_LISTEN_ADDR`
- `LRG_DB_PATH`

These values are currently sourced only from environment variables.

### Current station config

`internal/storage/sqlite/schema.go` already persists:

- station base URLs
- station API keys
- thresholds
- mappings
- statuses
- request logs
- failover events

### Current admin capability

`internal/admin/handlers.go` already provides:

- `/admin/stations`
- `/admin/mappings`
- `/admin/status`
- `/admin/logs`

This spec must extend the admin model without creating a second station configuration source.

## Target Runtime Storage Model

### Windows app data root

The Windows formal startup path uses:

`%LocalAppData%\LocalRelayGateway\`

### Runtime config file

Path:

`%LocalAppData%\LocalRelayGateway\runtime.json`

Version-one schema:

```json
{
  "local_api_key": "replace-this-key"
}
```

### Default database file

Path:

`%LocalAppData%\LocalRelayGateway\local-relay-gateway.db`

## Route Model

### New routes

- `/admin/setup`
- `/admin/runtime`

### Existing routes unchanged

- `/admin/stations`
- `/admin/mappings`
- `/admin/status`
- `/admin/logs`

## Page Responsibilities

### `/admin/setup`

Purpose:

- first-run initialization only
- recovery path for missing or invalid runtime config

Behavior:

- show one required field: `Local API Key`
- write config to `runtime.json`
- redirect to `/admin/stations` after successful save
- if runtime config already exists and is valid, do not act as a normal settings page
- on direct revisit after configuration exists, redirect to `/admin/runtime` or `/admin/stations`

### `/admin/runtime`

Purpose:

- long-term management page for the local gateway entrypoint only

Behavior:

- show local OpenAI base URL
- show local Anthropic base URL
- show current local API key in hidden form by default
- allow key show/hide
- allow key copy
- allow local OpenAI base URL copy
- allow local Anthropic base URL copy
- allow saving a new local API key
- show message that runtime changes require restart

Explicit boundary:

- `/admin/runtime` manages the local gateway identity and local client entrypoints
- `/admin/stations` manages upstream station configuration
- `/admin/mappings` manages alias-to-upstream model mapping

## Startup Modes

The process must distinguish exactly two modes.

### Setup mode

Entry condition:

- `runtime.json` does not exist
- or `runtime.json` exists but is unreadable, invalid, or missing `local_api_key`

Behavior:

- create app data directory if needed
- start the HTTP server on `127.0.0.1:8787`
- generate an in-memory temporary admin write token for this process
- auto-open browser to `/admin/setup`
- keep admin pages usable for initialization
- block normal relay usage until setup is finished and the process is restarted

### Normal mode

Entry condition:

- valid `runtime.json` exists
- `local_api_key` is non-empty

Behavior:

- load runtime config
- start the HTTP server on `127.0.0.1:8787`
- use persisted local API key
- auto-open browser to `/admin/stations`
- allow normal relay traffic

## Authentication Model Adjustment

The current implementation uses one write token source. First-run setup introduces a temporary state where a usable admin write token is needed before a persisted local client key exists.

The implementation should separate these concepts:

- local client API key
  - used by Codex CLI, Claude Code, and other clients
- admin form write token
  - used by the local admin UI for mutating requests

### Setup mode auth behavior

- admin pages use a process-local temporary write token
- relay endpoints reject usage because initialization is incomplete

### Normal mode auth behavior

- admin write token can continue to reuse the configured local API key, or a clearly derived equivalent if implementation prefers, but this must remain a single clear source
- relay endpoints use the persisted local API key

The implementation must avoid introducing two long-term independently managed credentials for normal operation.

## Browser Launch Behavior

Windows formal startup should open the browser only after the HTTP listener is ready.

Mode-specific target:

- setup mode -> `/admin/setup`
- normal mode -> `/admin/stations`

If browser launch fails:

- the process continues running
- the console prints the exact manual URL to open

Linux and macOS behavior remains unchanged in version one.

## Error Handling

### Missing runtime config

- normal setup-mode entry
- not treated as an error

### Invalid or corrupt runtime config

- log a clear message to the console
- enter recovery-style setup mode
- `/admin/setup` shows that re-saving will replace the invalid config

### App data directory creation failure

- startup fails immediately
- console prints target path and system error

### Port occupied

- startup fails immediately
- console prints that `127.0.0.1:8787` is already in use
- operator is told to stop the previous process first

### Browser launch failure

- does not fail startup
- console prints the manual URL

### Runtime key update from `/admin/runtime`

- save new key to `runtime.json`
- display success message
- display explicit restart-required message
- current process continues using the old key until restart

## User Flow

### First startup on Windows

1. User double-clicks `local-relay-gateway.exe`
2. Process checks `%LocalAppData%\LocalRelayGateway\runtime.json`
3. Config is missing
4. Process enters setup mode
5. Service starts on `127.0.0.1:8787`
6. Browser opens `/admin/setup`
7. User enters `Local API Key`
8. Config is saved
9. Browser redirects to `/admin/stations`
10. User configures stations and mappings
11. User restarts the gateway once
12. Local clients can now authenticate with the saved key

### Subsequent startup on Windows

1. User double-clicks `local-relay-gateway.exe`
2. Process finds valid runtime config
3. Process enters normal mode
4. Service starts on `127.0.0.1:8787`
5. Browser opens `/admin/stations`

## Data and Component Changes

### `internal/config`

Add a clear loader boundary:

- environment-driven loader for existing developer flow
- file-driven loader for Windows formal startup flow

The implementation should avoid ambiguous precedence rules. A future maintainer must be able to answer exactly which startup path produced which runtime values.

### `internal/admin`

Add:

- setup handler
- runtime handler
- view models for setup/runtime pages
- UI affordances for copy and key visibility

Must not change the ownership boundary of the existing stations page.

### `cmd/local-relay-gateway`

Add Windows startup orchestration:

- resolve app data paths
- determine startup mode
- start server
- wait until listener is ready
- open browser

The design should keep this orchestration explicit rather than burying it inside unrelated gateway packages.

## Testing Requirements

Implementation must add tests for:

- runtime config load from file
- missing config enters setup mode
- invalid config enters recovery setup mode
- setup save writes expected JSON
- runtime save updates config
- relay endpoints reject use during setup mode
- setup route redirect behavior after configuration exists
- browser launch helper behavior should be isolated behind a testable boundary if practical
- existing graceful shutdown behavior remains covered

Tests should preserve the current AI-first maintenance preference:

- explicit names
- one behavior per test
- no magic fixtures without explanation

## Documentation Requirements

After implementation, update:

- `README.md`
- `docs/usage.md`
- `AGENTS.md` only if the runtime source-of-truth description changes in a way future agents must know before editing

Docs must explain:

- Windows double-click startup
- first-run setup behavior
- runtime config file location
- database location for Windows formal startup
- restart requirement after changing local API key

## Risks

### Risk: duplicated config responsibilities

If `/admin/runtime` grows to include station data, the system will become harder to maintain and easier to break.

Mitigation:

- keep `/admin/runtime` strictly limited to local gateway runtime data

### Risk: inconsistent auth behavior during setup

If relay endpoints remain partially usable before initialization completes, operators may end up with confusing half-configured behavior.

Mitigation:

- hard-block relay traffic during setup mode

### Risk: unclear startup precedence

If Windows file-based config and environment-based config are mixed without a clear rule, future changes will cause hard-to-debug startup behavior.

Mitigation:

- make startup mode explicit and document the source of truth clearly

## Implementation Preference

Prefer the smallest change set that preserves clean boundaries:

- no second station page
- no background launcher process
- no hot reload
- no platform-specific encryption in version one
- no hidden fallback credentials

## Acceptance Criteria

- A Windows user can double-click the formal executable without manually setting environment variables
- First startup opens `/admin/setup`
- The user can save `Local API Key`
- The config persists under `%LocalAppData%\LocalRelayGateway\`
- Later startups open `/admin/stations`
- Existing station and mapping flows continue to work
- Runtime page provides copy and key-management affordances
- Setup mode blocks normal relay traffic
- Runtime key changes require restart and clearly say so
- Console shutdown via `Ctrl+C` still works
