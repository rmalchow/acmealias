#!/usr/bin/env bash
# Stops the dev stack. State under data/ is preserved — next up.sh resumes
# with the same database and the same Keycloak realm. Use destroy.sh to wipe.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_docker
# compose_all, not compose: `--profile acme` so the opt-in containers stop
# too, whether or not this shell is the one that started them.
say "stopping containers (data/ preserved)"
compose_all down --remove-orphans
say "dev stack down"
