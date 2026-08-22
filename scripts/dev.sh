#!/usr/bin/env bash
# The dev stack's front door: one verb per thing an operator wants to do.
#
#   ./scripts/dev.sh up [--acme]     start (and migrate), then print status
#   ./scripts/dev.sh down            stop, keeping the database and realm
#   ./scripts/dev.sh restart        down, then up — picks up compose/.env edits
#   ./scripts/dev.sh reset [--yes]   destroy + up + seed — the "fresh clone" button
#   ./scripts/dev.sh seed            sample project, load balancer, wildcard domain, API key
#   ./scripts/dev.sh issue           force a real DNS-01 issuance through the fake CA
#   ./scripts/dev.sh status          what is running, the URLs, the test logins
#   ./scripts/dev.sh logs [svc...]   follow logs; no argument means api and web
#   ./scripts/dev.sh destroy [--yes] stop and wipe every byte of state under data/
#   ./scripts/dev.sh watch [--acme]  up, then follow api + web — the working loop
#
# Nothing runs on the host: air and ng serve are containers with the source
# bind-mounted, so `watch` and `logs` start nothing of their own and Ctrl-C
# only DETACHES — the stack keeps running until `down`.
#
# Each verb is a thin delegation to development/scripts/<name>.sh, which is
# where the actual work and the explanatory comments live.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DEV_SCRIPTS="$REPO_DIR/development/scripts"

usage() {
  sed -n '2,13p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

cmd="${1:-}"
[ $# -gt 0 ] && shift

case "$cmd" in
  up)      "$DEV_SCRIPTS/up.sh" "$@" ;;
  down)    "$DEV_SCRIPTS/down.sh" ;;
  restart) "$DEV_SCRIPTS/down.sh"; "$DEV_SCRIPTS/up.sh" "$@" ;;
  reset)   "$DEV_SCRIPTS/reset.sh" "$@" ;;
  seed)    "$DEV_SCRIPTS/seed.sh" "$@" ;;
  issue)   "$DEV_SCRIPTS/issue.sh" ;;
  status)  "$DEV_SCRIPTS/status.sh" "$@" ;;
  destroy) "$DEV_SCRIPTS/destroy.sh" "$@" ;;
  logs)
    if [ $# -eq 0 ]; then "$DEV_SCRIPTS/logs.sh" api web; else "$DEV_SCRIPTS/logs.sh" "$@"; fi
    ;;
  watch)
    "$DEV_SCRIPTS/up.sh" "$@"
    say "console: http://acme-console.localhost   (Ctrl-C detaches, containers keep running)"
    "$DEV_SCRIPTS/logs.sh" api web
    ;;
  ""|-h|--help|help) usage 0 ;;
  *)       printf '\033[1;31merror\033[0m unknown command: %s\n\n' "$cmd" >&2; usage 1 ;;
esac
