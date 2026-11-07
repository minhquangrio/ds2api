#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

PY_BIN=""
if command -v python >/dev/null 2>&1; then
  PY_BIN="python"
elif command -v python3 >/dev/null 2>&1; then
  PY_BIN="python3"
fi

if [ -n "$PY_BIN" ]; then
  "$PY_BIN" -m pytest workers/muse -q "$@"
else
  if [ -n "${CI:-}" ]; then
    echo "ERROR: python not found in CI environment" >&2
    exit 1
  fi
  echo "python not found, skipping python worker tests"
fi
