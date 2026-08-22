#!/usr/bin/env bash
# Removes the dev stack AND every byte of its state: containers, networks,
# anonymous volumes, and the bind mounts under data/. The next up.sh starts
# from an empty database and re-imports the Keycloak realm.
#
# Pass --yes to skip the prompt (CI, or scripted resets).
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_docker

if [ "${1:-}" != "--yes" ]; then
  printf 'This deletes the dev database and Keycloak state under development/data/.\nContinue? [y/N] '
  read -r reply
  case "$reply" in [yY]*) ;; *) say "aborted"; exit 0 ;; esac
fi

say "removing containers and volumes"
compose_all down --volumes --remove-orphans 2>/dev/null || true

say "wiping data/"
remove_data_dirs

# The served zone is generated from the alias the seed created, so it is state
# too: leaving it behind would have the fake internet publish a delegation for
# a database that no longer exists. The next lib.sh renders an empty one.
rm -f "$DEV_DIR/$ZONE_FILE"

say "dev stack destroyed"
