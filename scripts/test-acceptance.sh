#!/usr/bin/env bash
# The acceptance gate: real Traefik, pebble as the CA, CoreDNS as the public
# internet, against this service — and assertions on all of it.
# See openspec/changes/add-acceptance-environment (D14, design A10).
#
# THIS SCRIPT IS RUN BY HAND, NOT BY THE PIPELINE. .gitlab-ci.yml declares no
# acceptance stage on purpose; the comment where the job used to be says why.
#
# The stack lifecycle belongs to TestMain in backend/acceptance: it runs
# ./scripts/dev.sh reset --yes, then up --acme, then down. Expect minutes.
# ACCEPTANCE_KEEP_STACK=1 reuses whatever is already running and skips both.
#
# Arguments are passed through to `go test`, so -run and -v work:
#   ./scripts/test-acceptance.sh -run TestWildcard -v
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need go; need docker
docker info >/dev/null 2>&1 || die "the docker daemon is not reachable"

( cd backend && go test -tags acceptance -timeout 60m ./acceptance/... "$@" )
