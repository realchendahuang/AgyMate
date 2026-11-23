#!/usr/bin/env bash
set -euo pipefail

resolve_bin() {
  if [[ -n "${AGYMATE_BIN:-}" && -x "${AGYMATE_BIN}" ]]; then
    printf '%s\n' "${AGYMATE_BIN}"
    return 0
  fi
  if command -v agymate >/dev/null 2>&1; then
    command -v agymate
    return 0
  fi
  for cand in \
    "${HOME}/.local/bin/agymate" \
    "/opt/homebrew/bin/agymate" \
    "/usr/local/bin/agymate" \
    "${HOME}/go/bin/agymate"; do
    if [[ -x "${cand}" ]]; then
      printf '%s\n' "${cand}"
      return 0
    fi
  done
  return 1
}

BIN="$(resolve_bin || true)"
if [[ -z "${BIN}" ]]; then
  echo "agymate binary not found. Install via Homebrew, scripts/get.sh, or run 'agymate setup'." >&2
  exit 1
fi

exec "${BIN}" mcp
