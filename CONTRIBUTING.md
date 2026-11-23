# Contributing to AgyMate

Thank you for your interest in contributing to AgyMate!

## Prerequisites

- **Go** (version matching [`go.mod`](go.mod)) — **no CGO or Xcode Command Line Tools required** (`CGO_ENABLED=0` across all platforms).
- **Node.js** (optional, for `make js-check` syntax validation).
- **Bun** (only needed if modifying `mcpapp-ui/src/*` to rebuild `internal/mcpapp/dist/app.html`).

## Repository Layout

```text
cmd/agymate/         CLI entrypoint, setup/uninstall/config/doctor commands
embeds.go            Embeds antigravity-plugin/ & magpie-plugin/ into the binary
frontend/            Embedded Web dashboard (vanilla HTML/CSS/JS)
internal/
  cdp/               DevToolsActivePort discovery & Shadow-DOM window HUD
  colab/             Multi-account Google Colab PKCE OAuth2 & CCU collector
  config/            Config, atomic persistence, 14-day quota state
  doctor/            Cross-platform health checks & Sidecar proxy
  hooks/             Non-destructive hooks.json merge & live session tracker
  hub/               Loopback HTTP API, SSE broadcaster & polling orchestrator
  instances/         Multi-instance discovery & isolated profile lifecycle
  mcpapp/            Stdio MCP server & embedded MCP App HTML (SEP-1865)
  notify/            Telegram Bot & universal Webhook notification dispatchers
  rpc/               Local Antigravity Connect-RPC client
  service/           macOS LaunchAgent & Linux systemd --user service manager
  setup/             Self-contained setup & clean uninstall engine
  telemetry/         Read-only SQLite protobuf parser, FTS5 search & pricing overrides
antigravity-plugin/  Antigravity native plugin manifest, SKILL & MCP config
magpie-plugin/       Optional Magpie sidebar page plugin
mcpapp-ui/           MCP App frontend source (bundled into internal/mcpapp/dist/app.html)
```

## Local Development Workflow

```bash
# Run vet, race-detector tests, and JS syntax check
make verify

# Verify Pure Go cross-compilation across macOS, Linux, and Windows
make cross-check

# Rebuild MCP App UI (only when editing mcpapp-ui/src/*)
make build-mcp-app

# Build local binary without touching system services
make build
./bin/agymate serve

# Preview setup actions safely
./bin/agymate setup --dry-run
```

## Pull Request Guidelines

1. Keep changes focused and well-tested (add unit tests for new behavior or bug fixes).
2. Preserve strict safety invariants:
   - Never relax loopback/Host/Origin/`X-AgyMate-Token` checks.
   - Never open Antigravity SQLite databases in write mode.
   - Never mutate official `main` / `ide` client lifecycles.
   - Keep `CGO_ENABLED=0` compilation green across macOS, Linux, and Windows.
3. Run `make verify && make cross-check` before opening a PR.
