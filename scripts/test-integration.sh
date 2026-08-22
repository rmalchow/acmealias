#!/usr/bin/env bash
# Database-backed tests. Needs a PostgreSQL: development/scripts/up.sh starts
# one, and this defaults to that instance. These do NOT run in CI by design
# (design D13) — they are a local pre-push guard.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need go

: "${TEST_DATABASE_URL:=postgres://acmeservice:acmeservice@localhost:5432/acmeservice_test?sslmode=disable}"
export TEST_DATABASE_URL
say "TEST_DATABASE_URL=${TEST_DATABASE_URL%%\?*}"

( cd backend && go test ./... -run Integration "$@" )
