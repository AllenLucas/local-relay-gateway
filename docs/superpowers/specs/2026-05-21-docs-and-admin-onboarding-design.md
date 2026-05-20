# Docs And Admin Onboarding Design

## Overview

Improve first-run usability for the local relay gateway by adding:

- A bilingual project-level `README.md`
- A more complete bilingual `docs/usage.md`
- A bilingual empty-state onboarding block on `/admin/stations`

This change is documentation and UI guidance only. It does not add automatic sample data, import/export flows, or a separate onboarding subsystem.

## Goals

- Make the repository usable as an open-source project landing page
- Give the user complete Windows-first setup steps for running the gateway
- Document station setup and model mapping for both Codex CLI and Claude Code
- Make an empty admin database self-explanatory on first launch
- Keep the admin UI lightweight and server-rendered

## Non-Goals

- Do not auto-create sample stations or mappings in SQLite
- Do not add a multi-step onboarding wizard
- Do not add new backend APIs
- Do not change routing, failover, or protocol behavior

## Recommended Approach

Use an empty-state guidance panel on the existing `/admin/stations` page and pair it with strong bilingual documentation.

Why this approach:

- Better than docs-only because users can act directly from the page where station creation happens
- Lower scope and maintenance cost than a dedicated wizard
- Preserves the current low-memory, server-rendered architecture

## Admin Empty-State Design

When there are no stations in the database, `/admin/stations` should show a bilingual onboarding panel above the existing station form.

The panel should include:

1. Step 1 / 第一步
- Start the gateway locally
- Mention the default local address

2. Step 2 / 第二步
- Explain the station form fields the user must fill
- Distinguish OpenAI upstream settings from Anthropic upstream settings

3. Step 3 / 第三步
- Explain that stations are created first
- Then model aliases are created under mappings

The panel should also include copy-friendly examples:

- Example station base URL placeholders
- Example aliases:
  - `gpt-5`
  - `claude-sonnet`
- Example upstream model mappings:
  - `gpt-5.1`
  - `claude-sonnet-4-5`

The panel should appear only when the station list is empty. Once at least one station exists, the page should behave like the normal management view without showing the onboarding block.

## Documentation Design

### README.md

Create a repository-root `README.md` as the main open-source project document. It should be bilingual in one file, with Chinese-first and English-following sections.

Recommended structure:

1. Project summary / 項目介紹
2. Features / 核心特性
3. Use cases / 適用場景
4. Architecture overview / 架構概覽
5. Quick start / 快速開始
6. Station configuration / 站點配置
7. Model alias mapping / 模型映射
8. Codex CLI setup / Codex CLI 配置
9. Claude Code setup / Claude Code 配置
10. Admin UI guide / Admin 頁面說明
11. Health, failover, cooldown / 健康檢查、切換、冷卻
12. Development and testing / 開發與測試
13. FAQ / 常見問題

### docs/usage.md

Expand `docs/usage.md` into an operator-oriented manual.

It should include:

- Startup commands on Windows
- Required environment variables
- How to open the admin UI
- How to create the first station
- How to create the first mappings
- Example values for OpenAI and Anthropic flows
- Codex CLI and Claude Code configuration examples
- A short troubleshooting section

## File-Level Changes

Planned files:

- Create: `README.md`
- Modify: `docs/usage.md`
- Modify: `internal/admin/viewmodels.go`
- Modify: `internal/admin/handlers.go`
- Modify: `internal/admin/templates/stations.gohtml`
- Modify: `internal/admin/handlers_test.go` if needed

## Data Model And Template Shape

`StationsPage` should gain enough fields to drive the onboarding panel without hard-coding large bilingual blocks in the handler logic.

Recommended shape:

- A boolean indicating empty-state visibility
- A small set of example values for aliases and upstream model names
- Optional default local address string if useful for template rendering

Keep the view model simple and template-focused.

## Testing

Add admin tests that verify:

- Empty station list shows the onboarding content
- Non-empty station list does not show the onboarding content

Verification should include:

- `go test ./internal/admin -v`
- `go test ./...`

## Risks

- Bilingual content can become noisy if repeated too aggressively
- Hard-coded examples can confuse users if they look like real provider endpoints

Mitigations:

- Keep prose concise
- Use clearly fake placeholder domains
- Reuse command and environment variable blocks instead of duplicating them
