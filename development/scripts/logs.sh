#!/usr/bin/env bash
# Follows logs for the whole stack, or one service: logs.sh api
#
# Covers the `acme` profile containers too — `logs.sh traefik-lb` is where an
# issuance failure shows up first.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
require_docker
compose_all logs -f --tail=100 "$@"
