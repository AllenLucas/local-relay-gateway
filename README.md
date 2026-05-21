# 本地 LLM 中转网关 / Local LLM Relay Gateway

轻量级、本地优先、SQLite 持久化的多中转站 LLM 网关，适合在 Windows、Linux 和 macOS 上为 Codex CLI、Claude Code 和其他脚本工具提供一个稳定的本地入口。  
Lightweight, localhost-first, SQLite-backed multi-relay LLM gateway for Windows, Linux, and macOS, designed to give Codex CLI, Claude Code, and local automation one stable endpoint.

## 项目概览 / Overview

这个项目把多套上游 OpenAI 和 Anthropic 中转站收敛到一个本地进程中，统一做模型别名映射、优先级选择、失败切换、冷却和本地管理。默认监听 `127.0.0.1:8787`，管理页面入口是 `/admin/stations`。  
This project collapses multiple upstream OpenAI and Anthropic relays into one local process, with model alias mapping, priority-based selection, failover, cooldown, and a local admin UI. The default listener is `127.0.0.1:8787`, and the admin entry point is `/admin/stations`.

| 路径 / Path | 用途 / Purpose |
| --- | --- |
| `/openai/v1/responses` | OpenAI Responses-compatible proxy |
| `/openai/v1/chat/completions` | OpenAI Chat Completions-compatible proxy |
| `/anthropic/v1/messages` | Anthropic Messages-compatible proxy |
| `/admin/stations` | Station management UI |
| `/healthz` | Local process health check |

## 核心特性 / Features

- 单个本地地址同时代理 OpenAI 和 Anthropic 兼容请求。 / One local address proxies both OpenAI-compatible and Anthropic-compatible requests.
- 按站点优先级选择上游，并在请求尚未向客户端输出时执行失败切换。 / Selects upstreams by station priority and fails over before client output has started.
- 通过模型别名把本地稳定名称映射到真实上游模型名。 / Maps stable local aliases to real upstream model names.
- 使用 SQLite 保存站点、映射、状态、请求日志和切换事件。 / Uses SQLite to persist stations, mappings, status, request logs, and failover events.
- 提供本地 Admin UI 进行站点、映射、状态和日志排查。 / Includes a local admin UI for station, mapping, status, and log inspection.

## 平台支持 / Platform Support

- 支持 Windows、Linux 和 macOS；核心运行时不依赖平台专属 GUI。 / Supports Windows, Linux, and macOS; the runtime does not depend on any platform-specific GUI stack.
- 管理界面是本地网页，三平台都通过浏览器访问同一套 `/admin/*` 页面。 / The admin UI is browser-based, so all three platforms use the same `/admin/*` pages.
- SQLite 采用纯 Go 驱动，默认不需要额外安装系统级 SQLite 库。 / SQLite uses a pure-Go driver, so no separate system SQLite library is normally required.

## 适用场景 / Use Cases

- 你希望 Codex CLI 永远连到同一个本地 `OPENAI_BASE_URL`，而不是直接面向具体中转站。 / You want Codex CLI to point at one stable local `OPENAI_BASE_URL` instead of a specific relay.
- 你需要让 Claude Code 通过本地 Anthropic 兼容入口工作，同时保留多上游切换能力。 / You need Claude Code to use a local Anthropic-compatible endpoint with multi-upstream failover.
- 你需要为脚本、测试或个人工作流提供低运维成本的本地网关。 / You want a low-overhead local gateway for scripts, tests, or personal workflows.

## 架构概览 / Architecture

- 本地进程读取运行时环境变量，启动 HTTP 服务并打开 SQLite 数据库。 / The local process reads runtime environment variables, starts the HTTP server, and opens the SQLite database.
- `Stations` 保存 OpenAI 与 Anthropic 上游地址、密钥和阈值。 / `Stations` store OpenAI and Anthropic upstream URLs, keys, and thresholds.
- `Mappings` 按 `station + protocol + alias` 把别名映射到真实模型名。 / `Mappings` translate aliases to real model names by `station + protocol + alias`.
- 路由器只会挑选已启用、存在对应映射、且状态为空或 `healthy` 的站点。 / The router only chooses stations that are enabled, mapped for the request, and empty-state or `healthy`.

## 快速开始 / Quick Start

先设置本地网关密钥并启动进程；`LRG_LISTEN_ADDR` 和 `LRG_DB_PATH` 是可选覆盖项。  
Set the local gateway key and start the process; `LRG_LISTEN_ADDR` and `LRG_DB_PATH` are optional overrides.

```powershell
$env:LRG_LOCAL_API_KEY="replace-this-key"
$env:LRG_LISTEN_ADDR="127.0.0.1:8787"
$env:LRG_DB_PATH=".\local-relay-gateway.db"
& "C:\Program Files\Go\bin\go.exe" run .\cmd\local-relay-gateway
```

```bash
export LRG_LOCAL_API_KEY="replace-this-key"
export LRG_LISTEN_ADDR="127.0.0.1:8787"
export LRG_DB_PATH="./local-relay-gateway.db"
go run ./cmd/local-relay-gateway
```

启动后打开这些页面：  
After startup, open these pages:

- `http://127.0.0.1:8787/admin/stations`
- `http://127.0.0.1:8787/healthz`

停止前台进程时，Windows、Linux 和 macOS 都优先在当前终端按 `Ctrl+C`，这样会触发优雅关闭并让 HTTP 服务正常退出。  
To stop the foreground process, prefer pressing `Ctrl+C` in the same terminal on Windows, Linux, and macOS so the server can shut down gracefully.

如果进程就是在当前前台终端里启动的，直接关闭那个终端通常也会结束进程；但这属于强制中断，优先还是用 `Ctrl+C`。  
If the process was started in the current foreground terminal, closing that terminal usually ends the process too, but that is a forced stop and `Ctrl+C` is still preferred.

## 站点配置 / Station Configuration

每个站点可以保存 OpenAI、Anthropic 或两者同时保存的上游配置；至少要完整填写一组协议。优先级数字越大越先被尝试。  
Each station can store OpenAI settings, Anthropic settings, or both; at least one protocol must be configured as a complete pair. Higher priority numbers are tried first.

推荐先建一个主站点，再按更低优先级补一个备用站点：  
Create one primary station first, then add a lower-priority backup station:

| 字段 / Field | 主站点示例 / Primary Example | 备用站点示例 / Backup Example | 说明 / Notes |
| --- | --- | --- | --- |
| `name` | `relay-a` | `relay-b` | 本地标识名 / Local identifier |
| `enabled` | `true` | `true` | 禁用站点不会被选中 / Disabled stations are never selected |
| `priority` | `100` | `50` | 数值越大越优先 / Higher value wins |
| `cooldown_seconds` | `30` | `30` | 触发冷却后跳过该站点的秒数 / Seconds skipped after cooldown triggers |
| `consecutive_failure_threshold` | `1` | `1` | 连续失败多少次进入冷却 / Failures before cooldown |
| `consecutive_recovery_threshold` | `2` | `2` | 连续恢复多少次回到健康 / Recoveries before healthy |
| `openai_base_url` | `https://relay-a.example.invalid/openai` | `https://relay-b.example.invalid/openai` | 网关会再拼接 `/v1/responses` 或 `/v1/chat/completions` / Gateway appends `/v1/responses` or `/v1/chat/completions` |
| `anthropic_base_url` | `https://relay-a.example.invalid/anthropic` | `https://relay-b.example.invalid/anthropic` | 网关会再拼接 `/v1/messages` / Gateway appends `/v1/messages` |

## 模型别名映射 / Model Alias Mapping

请求里的 `model` 不直接决定上游真实模型名，系统会先按协议和别名查找映射。没有映射的站点不会参与该请求。  
The request `model` does not go upstream directly. The system first resolves a mapping by protocol and alias. Stations without a matching mapping are skipped.

推荐先创建这些别名：  
Recommended starter aliases:

| 协议 / Protocol | 本地别名 / Local Alias | 上游模型 / Upstream Model |
| --- | --- | --- |
| `openai` | `gpt-5` | `gpt-5.1` |
| `anthropic` | `claude-sonnet` | `claude-sonnet-4-5` |

如果两个站点都要承接同一个别名，就要分别在各自站点下创建同名映射。  
If two stations should handle the same alias, create that alias mapping separately under each station.

## Codex CLI 配置 / Codex CLI Setup

Codex CLI 走 OpenAI 兼容入口，使用本地 `Authorization: Bearer` 令牌。  
Codex CLI uses the OpenAI-compatible entrypoint with the local `Authorization: Bearer` token.

```powershell
$env:OPENAI_BASE_URL="http://127.0.0.1:8787/openai/v1"
$env:OPENAI_API_KEY="replace-this-key"
```

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8787/openai/v1"
export OPENAI_API_KEY="replace-this-key"
```

## Claude Code 配置 / Claude Code Setup

Claude Code 走 Anthropic 兼容入口，使用同一个本地密钥。  
Claude Code uses the Anthropic-compatible entrypoint and the same local key.

```powershell
$env:ANTHROPIC_BASE_URL="http://127.0.0.1:8787/anthropic"
$env:ANTHROPIC_API_KEY="replace-this-key"
```

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787/anthropic"
export ANTHROPIC_API_KEY="replace-this-key"
```

## Admin UI 指南 / Admin UI Guide

- `/admin` 会重定向到 `/admin/stations`。 / `/admin` redirects to `/admin/stations`.
- `/admin/stations` 用来新增、查看和编辑站点。首次空库时会显示双语快速配置面板。 / `/admin/stations` creates, lists, and edits stations. It shows a bilingual quick-setup panel on an empty database.
- `/admin/mappings` 用来建立别名映射。 / `/admin/mappings` creates alias mappings.
- `/admin/status` 展示每个站点的状态、冷却时间、失败数和恢复数。 / `/admin/status` shows station state, cooldown time, failures, and recoveries.
- `/admin/logs` 展示请求日志、故障切换事件和用量统计。 / `/admin/logs` shows request logs, failover events, and usage summaries.

## 健康检查、切换与冷却 / Health, Failover, and Cooldown

- 只有在上游还没开始向客户端输出内容之前，系统才会切换到下一个站点。 / Failover only happens before an upstream has started sending output to the client.
- `429`、`5xx` 和传输错误会被当作失败并触发下一候选站点。 / `429`, `5xx`, and transport errors count as failures and trigger the next candidate station.
- 连续失败达到阈值后，站点会进入 `cooldown`，直到 `cooldown_seconds` 到期。 / Once the failure threshold is reached, the station enters `cooldown` until `cooldown_seconds` expires.
- 连续成功达到恢复阈值后，站点状态会回到 `healthy`。 / After enough consecutive recoveries, the station returns to `healthy`.
- 当前实现的后台健康检查会每 15 秒探测一次已配置协议；优先使用 `OpenAIBaseURL + /models`，否则回退到 `AnthropicBaseURL + /v1/messages`，HTTP 客户端超时固定为 5 秒。 / The current background health check probes the configured protocol every 15 seconds; it prefers `OpenAIBaseURL + /models` and otherwise falls back to `AnthropicBaseURL + /v1/messages`, with a fixed 5 second HTTP client timeout.

## 开发与测试 / Development and Testing

从源码运行和测试最直接；完整测试命令如下。  
Running and testing from source is the simplest path; the full test command is below.

```powershell
& "C:\Program Files\Go\bin\go.exe" test ./...
```

```bash
go test ./...
```

## 常见问题 / FAQ

**为什么请求返回 `401 unauthorized`？**  
通常是本地客户端配置的密钥和 `LRG_LOCAL_API_KEY` 不一致；OpenAI 路由需要 `Authorization: Bearer`，Anthropic 路由兼容 `x-api-key` 和 `Authorization: Bearer`。  
Usually the client key does not match `LRG_LOCAL_API_KEY`; OpenAI routes require `Authorization: Bearer`, and Anthropic routes accept either `x-api-key` or `Authorization: Bearer`.

**为什么提示 `no eligible upstream station`？**  
检查站点是否启用、对应协议是否存在已启用的别名映射、以及站点是否正处于 `cooldown`。  
Check whether the station is enabled, whether an enabled alias mapping exists for the protocol, and whether the station is currently in `cooldown`.

**站点 Base URL 应该写到哪一级？**  
OpenAI 站点通常填到 `.../openai`，Anthropic 站点通常填到 `.../anthropic`；网关会自动补上版本化路径。  
For OpenAI, the station URL should usually end at `.../openai`; for Anthropic, it should usually end at `.../anthropic`. The gateway appends the versioned route itself.

**数据库在哪里？**  
默认是工作目录下的 `local-relay-gateway.db`，可以用 `LRG_DB_PATH` 改成其他本地 SQLite 文件。  
By default it is `local-relay-gateway.db` in the working directory. Override it with `LRG_DB_PATH` to use a different local SQLite file.

**可以绑定到非 localhost 吗？**  
可以，通过 `LRG_LISTEN_ADDR` 修改；但这会让本地 Admin 和代理入口暴露到更广的网络范围，应该配合额外访问控制。  
Yes, via `LRG_LISTEN_ADDR`; but doing so exposes the admin and proxy endpoints more broadly and should be paired with additional access controls.
