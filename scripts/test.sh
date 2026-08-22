#!/usr/bin/env bash
# Unit tests, both halves. Database-backed tests skip unless TEST_DATABASE_URL
# is set — run scripts/test-integration.sh for those.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need go

say "go tests"
( cd backend && go test ./... "$@" )

if [ "${SKIP_FRONTEND:-}" = "1" ]; then
  warn "frontend tests skipped (SKIP_FRONTEND=1)"
  exit 0
fi

CHROME="${CHROME_BIN:-$(chrome_bin)}"
if [ -z "$CHROME" ]; then
  warn "no headless Chrome found — skipping frontend tests"
  warn "install chrome/chromium, or set CHROME_BIN, or pass SKIP_FRONTEND=1 to silence this"
  exit 0
fi

say "frontend tests"
( cd frontend && CHROME_BIN="$CHROME" npx ng test --watch=false --browsers=ChromeHeadless )
