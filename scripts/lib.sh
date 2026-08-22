#!/usr/bin/env bash
# Shared helpers for the top-level scripts. Sourced, never executed.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_DIR"

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warn\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is not installed${2:+ — $2}"; }

# Karma needs to be told where a headless Chrome is.
chrome_bin() {
  command -v google-chrome || command -v chromium || command -v chromium-browser || true
}
