#!/usr/bin/env bash
# Destroy, recreate and reseed in one step — the "put it back how it was on a
# fresh clone" button. This is what to run after editing realm/*.json or a
# migration, because Keycloak imports a realm only into an empty data
# directory and will otherwise keep serving the old one.
#
# Pass --yes to skip the confirmation prompt.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

"$DEV_DIR/scripts/destroy.sh" "${1:-}"
"$DEV_DIR/scripts/up.sh"
"$DEV_DIR/scripts/seed.sh"

say "dev stack reset"
