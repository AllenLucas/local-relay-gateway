# 操作手册 / Operator Guide

这份文档面向日常维护本地网关的操作者，覆盖 Windows、Linux 和 macOS 的启动、站点配置、模型映射和客户端接入流程。  
This guide is for day-to-day operators of the local gateway, covering startup, station configuration, model mappings, and client integration on Windows, Linux, and macOS.

## 系统摘要 / System Summary

- 默认监听地址是 `127.0.0.1:8787`。 / The default listen address is `127.0.0.1:8787`.
- 本地健康检查地址是 `http://127.0.0.1:8787/healthz`。 / The local process health endpoint is `http://127.0.0.1:8787/healthz`.
- OpenAI 兼容入口是 `/openai/v1/responses` 和 `/openai/v1/chat/completions`。 / OpenAI-compatible entrypoints are `/openai/v1/responses` and `/openai/v1/chat/completions`.
- Anthropic 兼容入口是 `/anthropic/v1/messages`。 / The Anthropic-compatible entrypoint is `/anthropic/v1/messages`.
- 管理页面默认从 `/admin/stations` 开始。 / The admin UI starts at `/admin/stations`.
- 数据持久化使用本地 SQLite 文件。 / Persistence uses a local SQLite file.

## 平台前提 / Platform Prerequisites

- 三个平台都需要可用的 Go 环境。 / All three platforms need a working Go installation.
- 默认使用浏览器访问管理页面，不依赖桌面 GUI 框架。 / The admin UI is browser-based and does not require a desktop GUI framework.
- 默认数据库是工作目录下的 SQLite 文件。 / The default database is a SQLite file in the working directory.

## Windows 启动 / Windows Startup

建议先在 PowerShell 会话里设置环境变量，再直接从源码启动。只想临时运行时，用 `$env:` 即可；需要长期保存时再改系统级环境变量。  
Set environment variables in your PowerShell session first, then run from source. Use `$env:` for temporary sessions; switch to persistent system variables only if you need them.

```powershell
$env:LRG_LOCAL_API_KEY="replace-this-key"
$env:LRG_LISTEN_ADDR="127.0.0.1:8787"
$env:LRG_DB_PATH=".\local-relay-gateway.db"
& "C:\Program Files\Go\bin\go.exe" run .\cmd\local-relay-gateway
```

启动成功后，先确认本地健康检查和 Admin 页面可访问：  
After the process starts, verify both the local health check and the admin UI:

- `http://127.0.0.1:8787/healthz`
- `http://127.0.0.1:8787/admin/stations`

## Linux / macOS 启动 / Linux and macOS Startup

在 `bash`、`zsh` 或兼容 shell 中先导出环境变量，再从源码启动。  
Export environment variables in `bash`, `zsh`, or a compatible shell, then start from source.

```bash
export LRG_LOCAL_API_KEY="replace-this-key"
export LRG_LISTEN_ADDR="127.0.0.1:8787"
export LRG_DB_PATH="./local-relay-gateway.db"
go run ./cmd/local-relay-gateway
```

启动成功后，同样检查：  
After startup, check the same endpoints:

- `http://127.0.0.1:8787/healthz`
- `http://127.0.0.1:8787/admin/stations`

## 环境变量 / Environment Variables

| 变量 / Variable | 默认值 / Default | 作用 / Purpose |
| --- | --- | --- |
| `LRG_LOCAL_API_KEY` | `change-me-local-key` | 本地网关认证密钥；OpenAI 本地入口读 `Authorization: Bearer`，Anthropic 本地入口兼容 `x-api-key` 和 `Authorization: Bearer` / Local gateway auth key; the OpenAI local entrypoint reads `Authorization: Bearer`, and the Anthropic local entrypoint accepts both `x-api-key` and `Authorization: Bearer` |
| `LRG_LISTEN_ADDR` | `127.0.0.1:8787` | 本地监听地址 / Local listen address |
| `LRG_DB_PATH` | `local-relay-gateway.db` | SQLite 数据库路径 / SQLite database path |

## 第一次打开 Admin / First Admin Session

首次空库启动时，`/admin/stations` 页面会在表单上方显示双语快速配置面板。  
On a first run with an empty database, `/admin/stations` shows a bilingual quick-setup panel above the form.

当前实现里，`/admin` 会重定向到 `/admin/stations`，其余常用页面是：  
In the current build, `/admin` redirects to `/admin/stations`, and the other useful pages are:

- `/admin/mappings`
- `/admin/status`
- `/admin/logs`

## 创建第一个站点 / Create The First Station

推荐先创建一个主站点 `relay-a`。如果你准备做故障切换，再补一个更低优先级的 `relay-b`。  
Create one primary station such as `relay-a` first. Add a lower-priority `relay-b` later if you want failover.

操作顺序：  
Operator sequence:

1. 打开 `http://127.0.0.1:8787/admin/stations`。  
   Open `http://127.0.0.1:8787/admin/stations`.
2. 在表单中填写站点名称和上游信息。  
   Fill in the station name and upstream settings.
   你可以只填 OpenAI、只填 Anthropic，或两者都填；但同一种协议的 `base_url` 和 `api_key` 必须成对出现。  
   You can fill OpenAI only, Anthropic only, or both; but each protocol's `base_url` and `api_key` must be provided together.
3. 保持 `Enabled` 勾选。  
   Keep `Enabled` checked.
4. 先用保守阈值跑通请求，再按观察结果调整。  
   Start with conservative thresholds, then tune after you observe real traffic.
5. 点击 `Save Station`。  
   Click `Save Station`.

下面这组值适合第一个主站点：  
These values are suitable for the first primary station:

| 字段 / Field | 示例值 / Example | 说明 / Notes |
| --- | --- | --- |
| `name` | `relay-a` | 本地显示名称 / Local display name |
| `enabled` | `true` | 必须启用才会参与路由 / Must be enabled to participate in routing |
| `priority` | `100` | 数值越大越优先 / Higher value is preferred |
| `cooldown_seconds` | `30` | 进入冷却后的跳过时间 / Skip time after cooldown triggers |
| `health_check_interval_seconds` | `15` | 会保存到站点记录中 / Stored with the station record |
| `health_check_timeout_seconds` | `5` | 会保存到站点记录中 / Stored with the station record |
| `consecutive_failure_threshold` | `1` | 达到后进入冷却 / Enter cooldown after this many failures |
| `consecutive_recovery_threshold` | `2` | 达到后恢复为健康 / Return to healthy after this many recoveries |
| `openai_base_url` | `https://relay-a.example.invalid/openai` | 网关会自动补 `/v1/responses` 或 `/v1/chat/completions` / Gateway appends `/v1/responses` or `/v1/chat/completions` |
| `openai_api_key` | `sk-relay-a-placeholder` | 上游 OpenAI 兼容密钥 / Upstream OpenAI-compatible key |
| `anthropic_base_url` | `https://relay-a.example.invalid/anthropic` | 网关会自动补 `/v1/messages` / Gateway appends `/v1/messages` |
| `anthropic_api_key` | `ak-relay-a-placeholder` | 上游 Anthropic 兼容密钥 / Upstream Anthropic-compatible key |

如果要增加备用站点，把 `priority` 降低，例如 `50`，其余按该站点支持的协议做同样配置即可。  
For a backup station, lower `priority` to something like `50` and fill the rest the same way for whichever protocol(s) that station supports.

已保存的站点后续可以在 `Stations` 页面点击 `Edit` 继续修改。  
Saved stations can be modified later by clicking `Edit` on the `Stations` page.

## 创建第一个映射 / Create The First Mappings

站点保存后，下一步不是直接发请求，而是先建立模型别名映射；没有映射的协议请求会直接失去候选站点。  
After saving a station, do not jump straight to client traffic. Create model alias mappings first; requests without a mapping lose all eligible candidates.

操作顺序：  
Operator sequence:

1. 打开 `http://127.0.0.1:8787/admin/mappings`。  
   Open `http://127.0.0.1:8787/admin/mappings`.
2. 选择刚创建的站点。  
   Select the station you just created.
3. 只为当前站点实际启用的协议创建映射。  
   Create mappings only for the protocols actually configured on that station.
4. 保持 `Enabled` 勾选。  
   Keep `Enabled` checked.

推荐的起始映射如下：  
Recommended starter mappings:

| 站点 / Station | 协议 / Protocol | 别名 / Alias | 上游模型 / Upstream Model |
| --- | --- | --- | --- |
| `relay-a` | `openai` | `gpt-5` | `gpt-5.1` |
| `relay-a` | `anthropic` | `claude-sonnet` | `claude-sonnet-4-5` |

如果你有多个站点都要处理同一个别名，必须在每个站点下各建一条映射。  
If multiple stations should serve the same alias, create that alias mapping under every participating station.

## Codex CLI 设置 / Codex CLI Setup

Codex CLI 通过本地 OpenAI 兼容入口访问网关。  
Codex CLI reaches the gateway through the local OpenAI-compatible endpoint.

```powershell
$env:OPENAI_BASE_URL="http://127.0.0.1:8787/openai/v1"
$env:OPENAI_API_KEY="replace-this-key"
```

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8787/openai/v1"
export OPENAI_API_KEY="replace-this-key"
```

发到 `/openai/v1/responses` 或 `/openai/v1/chat/completions` 的请求，会先用 `model` 字段查别名，再改写成真实上游模型名。  
Requests sent to `/openai/v1/responses` or `/openai/v1/chat/completions` resolve the alias from the `model` field and then rewrite it to the real upstream model name.

## Claude Code 设置 / Claude Code Setup

Claude Code 通过本地 Anthropic 兼容入口访问网关。  
Claude Code reaches the gateway through the local Anthropic-compatible endpoint.

```powershell
$env:ANTHROPIC_BASE_URL="http://127.0.0.1:8787/anthropic"
$env:ANTHROPIC_API_KEY="replace-this-key"
```

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787/anthropic"
export ANTHROPIC_API_KEY="replace-this-key"
```

如果你使用的是 `cc switch` 之类的包装器，也可以让它写入 `ANTHROPIC_AUTH_TOKEN`；当前网关兼容 `x-api-key` 和 `Authorization: Bearer` 两种本地鉴权头。  
If you use a wrapper such as `cc switch`, it can also write `ANTHROPIC_AUTH_TOKEN`; the gateway accepts both `x-api-key` and `Authorization: Bearer` for local Anthropic authentication.

发到 `/anthropic/v1/messages` 的请求会保留或补默认 `anthropic-version`，并把本地别名换成真实上游模型。  
Requests sent to `/anthropic/v1/messages` preserve the provided `anthropic-version` header or fall back to the default, then swap the local alias for the real upstream model.

## 手动验证请求 / Manual Verification Requests

如果你想在客户端接入前手动验证，可以直接在 PowerShell 里发一个 OpenAI 和一个 Anthropic 示例请求。  
If you want to validate manually before pointing clients at the gateway, send one OpenAI example and one Anthropic example from PowerShell.

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8787/openai/v1/responses" `
  -Headers @{ Authorization = "Bearer replace-this-key" } `
  -ContentType "application/json" `
  -Body '{"model":"gpt-5","input":"hello","stream":false}'

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8787/anthropic/v1/messages" `
  -Headers @{ "x-api-key" = "replace-this-key"; "anthropic-version" = "2023-06-01" } `
  -ContentType "application/json" `
  -Body '{"model":"claude-sonnet","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}'
```

Linux/macOS 下可以用 `curl`：  
On Linux and macOS, you can use `curl`:

```bash
curl -X POST "http://127.0.0.1:8787/openai/v1/responses" \
  -H "Authorization: Bearer replace-this-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5","input":"hello","stream":false}'

curl -X POST "http://127.0.0.1:8787/anthropic/v1/messages" \
  -H "x-api-key: replace-this-key" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}'
```

## Admin 页面说明 / Admin Pages

- `Stations`：新增站点、查看当前已保存的基础配置，并通过 `Edit` 修改已有站点。 / `Stations`: create stations, review saved base settings, and modify existing stations via `Edit`.
- `Mappings`：按站点和协议维护别名到上游模型名的映射。 / `Mappings`: manage alias-to-upstream-model mappings by station and protocol.
- `Status`：查看 `healthy`、`cooldown` 等状态，以及失败数、恢复数和最后错误。 / `Status`: inspect `healthy`, `cooldown`, and related counters and errors.
- `Logs`：查看最近请求、故障切换事件、按站点统计和按别名统计。 / `Logs`: inspect recent requests, failover events, usage by station, and usage by alias.

## 故障切换、健康检查与冷却 / Failover, Health, and Cooldown

- 路由顺序先看 `priority`，再看站点是否启用、是否存在映射、状态是否允许。 / Routing order starts with `priority`, then checks whether the station is enabled, mapped, and in an allowed state.
- 请求失败的判定包括传输错误、`429` 和 `5xx`。 / Failures include transport errors, `429`, and `5xx`.
- 一旦上游开始向客户端写入响应，当前请求就不会再切换到别的站点。 / Once an upstream has started writing to the client, that request will not fail over again.
- 站点达到 `consecutive_failure_threshold` 后进入 `cooldown`，到期前不会被选中。 / A station enters `cooldown` after reaching `consecutive_failure_threshold` and is skipped until cooldown expires.
- 站点达到 `consecutive_recovery_threshold` 后回到 `healthy`。 / A station returns to `healthy` after reaching `consecutive_recovery_threshold`.
- 当前后台健康检查固定每 15 秒执行一次，优先对 `OpenAIBaseURL + /models` 发起探测；如果站点只配置了 Anthropic，则改为请求 `AnthropicBaseURL + /v1/messages`。请求超时固定为 5 秒。 / The current background health loop runs every 15 seconds, preferring `OpenAIBaseURL + /models`; if a station is Anthropic-only, it instead probes `AnthropicBaseURL + /v1/messages`. The timeout remains fixed at 5 seconds.

## 开发与测试 / Development and Testing

文档改动本身不需要测试，但仓库的标准验证方式仍然是完整 Go 测试。  
Markdown-only changes do not require tests, but the repository's standard verification path is still the full Go test suite.

```powershell
& "C:\Program Files\Go\bin\go.exe" test ./...
```

```bash
go test ./...
```

## 故障排查 / Troubleshooting

**Admin 页面能打开，但请求一直 `401`。**  
先确认客户端环境变量中的密钥和 `LRG_LOCAL_API_KEY` 完全一致。OpenAI 本地入口读 `Authorization: Bearer`，Anthropic 本地入口兼容 `x-api-key` 和 `Authorization: Bearer`。  
First confirm that the client-side key exactly matches `LRG_LOCAL_API_KEY`. The OpenAI local route reads `Authorization: Bearer`, while the Anthropic local route accepts both `x-api-key` and `Authorization: Bearer`.

**保存了站点，但请求报 `no eligible upstream station`。**  
通常是映射未创建、映射未启用、协议填错，或者站点正处于 `cooldown`。去 `/admin/mappings` 和 `/admin/status` 对照检查。  
This usually means the mapping is missing, disabled, created under the wrong protocol, or the station is currently in `cooldown`. Check `/admin/mappings` and `/admin/status`.

**OpenAI 请求通了，Anthropic 请求不通。**  
确认这个站点是否真的配置了 `anthropic_base_url` 和 `anthropic_api_key`，并且另外创建了 `anthropic` 协议映射；`OpenAI-only` 站点不会参与 Anthropic 路由。  
Confirm that this station really has `anthropic_base_url` and `anthropic_api_key` configured and also has an `anthropic` mapping; an `OpenAI-only` station will not participate in Anthropic routing.

**状态页里长期显示 `cooldown`。**  
先检查上游是否持续返回 `429` 或 `5xx`，再检查对应协议的健康探测地址是否可访问：OpenAI 站点看 `.../models`，Anthropic-only 站点看 `.../v1/messages`。  
First check whether the upstream keeps returning `429` or `5xx`, then verify that the health probe endpoint for that protocol is reachable: `.../models` for OpenAI stations and `.../v1/messages` for Anthropic-only stations.

**数据库文件位置不对。**  
默认文件名是 `local-relay-gateway.db`；如果希望放到其他盘符或目录，显式设置 `LRG_DB_PATH`。  
The default database file is `local-relay-gateway.db`; set `LRG_DB_PATH` explicitly if you want it on another drive or directory.
