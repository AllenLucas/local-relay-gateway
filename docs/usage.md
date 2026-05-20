# Local Relay Gateway Usage

## Start The Gateway

```powershell
$env:LRG_LOCAL_API_KEY="replace-this-key"
go run .\cmd\local-relay-gateway
```

The gateway listens on `http://127.0.0.1:8787` by default.

## Codex CLI

Use the OpenAI-compatible base URL and your local bearer token:

```powershell
$env:OPENAI_BASE_URL="http://127.0.0.1:8787/openai/v1"
$env:OPENAI_API_KEY="replace-this-key"
```

Codex requests that hit `/chat/completions` and `/responses` will be routed through the local gateway.

## Claude Code

Use the Anthropic-compatible base URL and the same local key:

```powershell
$env:ANTHROPIC_BASE_URL="http://127.0.0.1:8787/anthropic"
$env:ANTHROPIC_API_KEY="replace-this-key"
```

Claude Code requests to `/v1/messages` will authenticate with `x-api-key` locally and be forwarded with the mapped upstream model.
