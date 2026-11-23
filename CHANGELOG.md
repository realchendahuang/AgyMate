# Changelog

All notable changes to AgyMate are documented in this file.

## [0.3.0] - 2026-11-22

### Added
- **Self-contained `agymate setup` & `agymate uninstall`**: Embedded `antigravity-plugin/` and `magpie-plugin/` into the Go binary; supports `--dry-run`, `--minimal`, granular component flags, non-destructive hook removal, and `--purge`.
- **`agymate config` subcommands**: `path`, `show [--unmasked]`, `set <key> <value>`, and `reset --yes`.
- **Universal Webhook Notifications**: Auto-detects Slack, Discord, Feishu/Lark, DingTalk, WeCom, Bark, or generic JSON with optional HMAC-SHA256 signing (`X-AgyMate-Signature`), configurable from both CLI and Web Settings.
- **Hot-reloaded Custom Model Pricing (`~/.config/agymate/pricing.json`)**: Override or add model enum names and per-1M token rates without recompiling.
- **Redacted Diagnostics (`agymate doctor --redact`)**: Strips home directories and email prefixes for safe sharing in bug reports.
- **Custom Colab OAuth Client Override**: Supports `AGYMATE_COLAB_CLIENT_ID` and `AGYMATE_COLAB_CLIENT_SECRET`.
- **Open-Source Release Automation**: Added `.goreleaser.yaml`, GitHub Actions CI/Release workflows, `scripts/get.sh` checksum-verified installer, `config.schema.json`, bilingual READMEs, `SECURITY.md`, `CONTRIBUTING.md`, and `NOTICE`.

### Changed
- **100% Pure Go (`CGO_ENABLED=0`)**: Replaced deprecated CoreServices CGO calls in `internal/doctor/doctor_darwin.go` with native macOS `plutil` + AppKit `NSWorkspace` JXA, enabling zero-CGO cross-compilation across macOS, Linux, and Windows.
- **Cross-Platform Process Discovery**: Replaced Unix-only `ps -axo` with POSIX `ps -eo` on Unix and PowerShell `Win32_Process` JSON enumeration on Windows.
- **Portable Binary Resolution**: `install-hooks`, Sidecar registration, and `run-mcp.sh` now resolve the active executable / `PATH` / Homebrew paths cleanly instead of assuming `~/.local/bin/agymate`.
- **Simplified `scripts/install.sh`**: Now compiles the Pure Go binary and delegates all setup steps to `agymate setup`.

### Fixed
- Fixed `.gitignore` accidentally ignoring `internal/mcpapp/dist/app.html` (`dist/` → `/dist/`), preventing broken clean builds without Bun.
- Fixed lock race and unmasked credential echo in `PUT /api/config` (`handlePutConfig`).
- Removed leftover scratch file and desktop wrapper references.

## [0.2.0] - 2026-11-18

### Added
- Antigravity v2.34+ native plugin & MCP App (`agymate_hub`) with sidebar, auxiliary pane, and inline card support.
- Multi-account Google Colab PKCE OAuth2 compute pool monitoring and VM teardown (`agymate colab`).
- Per-account aggregation with offline quota snapshots and 14-day compacted quota trends.
- Cross-platform background service management (`launchd` / `systemd --user`) and `X-AgyMate-Token` CSRF protection.
- Magpie page plugin with offline health preflight and theme variable syncing.

## [0.1.0] - 2026-11-14

### Added
- Initial release with local Hub, CLI, read-only SQLite token telemetry, CDP window HUD, isolated multi-profile management, lifecycle Hooks, and Telegram notifications.
