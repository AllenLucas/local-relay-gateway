# Changelog

All notable changes to this project are documented in this file.
本项目的所有重要变更都会记录在这份文件中。

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-05-22

### Added
- Local LLM relay gateway with OpenAI and Anthropic compatible endpoints, SQLite persistence, and a browser-based admin UI under `/admin/*`.
- Windows double-click bootstrap mode that creates `%LocalAppData%\LocalRelayGateway\`, listens on `127.0.0.1:8787`, and opens `/admin/setup` automatically.
- Station management with priority, cooldown, consecutive failure / recovery thresholds, and per-station OpenAI / Anthropic upstream configuration.
- Editable model alias mappings scoped by station and protocol.
- Background health probe (15s interval, 5s timeout) for the configured upstream protocol.
- Manual WebDAV configuration sync that uploads / pulls station and mapping snapshots into a fixed `allenlucasAIProxyTool/` child directory and keeps the latest 5.
- Forwarding of the client `User-Agent` to upstream; when the client omits it, the Go default `User-Agent` is suppressed.
- Bilingual (zh-CN / en) admin UI and documentation.

### Documentation
- `README.md`: bilingual project overview, quick start, station / mapping / admin guide, FAQ.
- `docs/usage.md`: bilingual operator guide covering Windows / Linux / macOS startup, configuration, WebDAV sync, and troubleshooting.

[Unreleased]: https://github.com/AllenLucas/local-relay-gateway/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/AllenLucas/local-relay-gateway/releases/tag/v0.1.0
