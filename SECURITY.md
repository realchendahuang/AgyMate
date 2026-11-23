# Security Policy

## Security & Privacy Architecture

AgyMate is designed as a **100% local, zero-telemetry** companion tool:

1. **Loopback-Only HTTP Hub**
   - Default binding is `127.0.0.1:3778`.
   - Every request passes `securityGuard`: non-loopback remote IPs, non-local `Host` headers, and non-local `Origin` headers are rejected with `403 Forbidden`.
   - All mutating endpoints (`POST` / `PUT` / `DELETE`) require a per-installation 256-bit random token stored in `~/.config/agymate/hub.token` (`0600`) via `X-AgyMate-Token` or `Authorization: Bearer`.
   - Unauthenticated `GET /api/config` and all `PUT /api/config` responses redact stored credentials (`telegram.botToken`, `webhook.secret`).

2. **Strictly Read-Only Telemetry**
   - Local Antigravity SQLite WAL databases are opened exclusively with `mode=ro&_pragma=query_only(1)`.
   - macOS Keychain checks inspect metadata attributes only (`security find-generic-password` without `-w`) and never read or export secrets.

3. **Credential Hygiene & Permissions**
   - Configuration (`config.json`), local Hub token (`hub.token`), Colab OAuth tokens (`colab_accounts.json`), and isolated profile metadata are written atomically with `0600` permissions inside `0700` directories.
   - Profile inheritance (`agymate profile create --inherit host`) strips sensitive tokens and redacts secret-like keys when copying MCP server configurations.
   - Official `main` and `ide` slots are strictly protected against lifecycle stop/delete/heal mutations.

4. **Redacted Diagnostics**
   - Use `agymate doctor --redact --json` when filing public bug reports so local home directories and email prefixes are redacted automatically.

## Reporting a Vulnerability

If you discover a security vulnerability (such as CSRF/Origin bypass, credential leakage, command injection, or unsafe file deletion):

- **Please do NOT open a public GitHub Issue.**
- Report privately via GitHub Security Advisories on this repository, or contact the maintainer privately with reproduction steps and impact assessment.
- We aim to acknowledge reports within 72 hours and publish a fix before public disclosure.
