#!/usr/bin/env bash
# Production build: the Angular bundle first, because the Go build embeds it.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need go; need npx

say "building console"
( cd frontend && npx ng build admin )

say "building binary"
( cd backend && go build ./... )

say "built"
