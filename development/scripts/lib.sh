#!/usr/bin/env bash
# Shared helpers for the dev stack scripts. Sourced, never executed.
#
# Every script cd's to development/ so compose.yaml, .env and data/ resolve
# the same way no matter where the operator invoked it from.

set -euo pipefail

DEV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_DIR="$(cd "$DEV_DIR/.." && pwd)"
cd "$DEV_DIR"

# Everything the stack keeps under ./data/<name>: service state AND the build
# caches, which live here rather than in the source tree so container-created,
# root-owned artefacts never land in backend/ or frontend/. Keep this list in
# step with compose.yaml — up.sh creates these before start (a missing bind
# mount point is an error for postgres, and an empty one is how `npm ci` and
# the Go caches are expected to begin), destroy.sh removes them.
#
# A first up.sh after a destroy.sh is therefore slow: empty module cache,
# empty build cache, full `npm ci`. A warm one is fast. If it is not,
# something is writing outside these mounts and that is worth finding.
DATA_DIRS=(postgres authentik_media redis mailpit go_mod go_build air_tmp node_modules npm_cache ng_cache_console ng_cache_admin pebble traefik_lb)

# The API key handoff: seed.sh writes HTTPREQ_USERNAME/HTTPREQ_PASSWORD here
# and traefik-lb reads it as an env_file. Compose refuses to run at all when
# an env_file is missing — including `down` — so it is created empty, always.
KEY_ENV="data/acme-key.env"

# The fake internet's example.com, split in two: a checked-in template a human
# reads and diffs, and the generated file CoreDNS actually serves. Only the
# challenge delegation differs between them, and it cannot be checked in — the
# shipped default generates a random alias label, so the CNAME target is only
# knowable from the running system (seed.sh reads it back). The generated file
# is gitignored.
ZONE_TMPL="dns/zones/example.com.zone.tmpl"
ZONE_FILE="dns/zones/example.com.zone"

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warn\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror\033[0m %s\n' "$*" >&2; exit 1; }

# Profile handling. `compose` runs with whatever profiles up.sh turned on;
# `compose_all` always includes `acme`, so down/destroy/logs/status reach the
# opt-in containers whether or not this shell is the one that started them.
# Compose interpolates these into the services that run our code, so the caches
# under data/ belong to the developer instead of to root.
#
# DEV_UID rather than UID: bash makes UID readonly, so assigning it aborts the
# script with "readonly variable" — and it does not export it either, so compose
# would never have seen it. GID is not a bash variable at all.
export DEV_UID="${DEV_UID:-$(id -u)}"
export DEV_GID="${DEV_GID:-$(id -g)}"

COMPOSE_PROFILE_ARGS=()
compose()     { docker compose -f "$DEV_DIR/compose.yaml" ${COMPOSE_PROFILE_ARGS[@]+"${COMPOSE_PROFILE_ARGS[@]}"} "$@"; }
compose_all() { docker compose -f "$DEV_DIR/compose.yaml" --profile acme "$@"; }

enable_acme_profile() { COMPOSE_PROFILE_ARGS=(--profile acme); }

# True when the opt-in stack is actually up, asked of docker rather than of a
# marker file that can lie.
acme_running() {
  compose_all ps --status running --format '{{.Service}}' 2>/dev/null | grep -qx pebble
}

# Never let a missing env_file break an unrelated compose command.
ensure_key_env() {
  mkdir -p "$DEV_DIR/data"
  [ -f "$DEV_DIR/$KEY_ENV" ] && return 0
  cat >"$DEV_DIR/$KEY_ENV" <<'EOF'
# Placeholder. development/scripts/seed.sh replaces this with the API key it
# mints, captured from `adminctl key create` — which prints the secret exactly
# once. Empty values here mean traefik-lb will be refused 401 by /present.
HTTPREQ_USERNAME=
HTTPREQ_PASSWORD=
EOF
}
ensure_key_env

# render_zone [record-line...] — write the served zone from the template,
# replacing the @@CHALLENGE_DELEGATION@@ marker with the given records (one
# per line). With no arguments the delegation is empty, which is a zone
# CoreDNS loads happily: names under it simply do not resolve until a seed
# publishes them.
render_zone() {
  local body
  if [ $# -eq 0 ]; then
    body="; none yet — development/scripts/seed.sh writes these when it seeds the inventory."
  else
    body="$(printf '%s\n' "$@")"
  fi
  awk -v body="$body" '/@@CHALLENGE_DELEGATION@@/ { print body; next } { print }' \
    "$DEV_DIR/$ZONE_TMPL" >"$DEV_DIR/$ZONE_FILE.new"
  mv "$DEV_DIR/$ZONE_FILE.new" "$DEV_DIR/$ZONE_FILE"
}

# A fresh clone has never seeded, and CoreDNS refuses to start on a missing
# zone file — so render an empty delegation before any compose command can
# run, the same way ensure_key_env writes an empty key file. A rendered zone
# is never overwritten here; only seed.sh replaces one.
ensure_zone() { [ -f "$DEV_DIR/$ZONE_FILE" ] || render_zone; }
ensure_zone

require_docker() {
  command -v docker >/dev/null 2>&1 || die "docker is not installed"
  docker info >/dev/null 2>&1 || die "the docker daemon is not reachable"
}

make_data_dirs() {
  # Postgres refuses to start on a missing mount point rather than creating
  # one, so these must exist before `compose up`.
  for d in "${DATA_DIRS[@]}"; do mkdir -p "$DEV_DIR/data/$d"; done

  # The two mount points that live INSIDE the bind-mounted source trees:
  # ./data/air_tmp lands on /src/tmp and ./data/ng_cache on /app/.angular/cache.
  # Docker creates a missing mount point itself, as root, which is exactly the
  # root-owned directory in backend/ we are trying not to have. Creating them
  # here means they belong to the developer instead. Both are gitignored and
  # stay empty — the contents live under data/.
  mkdir -p "$REPO_DIR/backend/tmp" "$REPO_DIR/frontend/.angular/cache"
}

remove_data_dirs() {
  # plain rm -rf cannot touch data/postgres: the postgres entrypoint chowns
  # PGDATA to a container-internal uid, which lands on the host as a uid the
  # invoking user does not own. Delete through a throwaway root container
  # rather than asking anyone to sudo.
  # The node and go images run as root too, so their caches under data/ land
  # root-owned on the host for the same reason. Same fix covers all of them.
  docker run --rm -v "$DEV_DIR/data:/data" alpine:3 \
    sh -c 'for d in "$@"; do rm -rf "/data/$d"; done' -- "${DATA_DIRS[@]}" "${KEY_ENV#data/}"
}

# wait_running <service> [timeout-seconds]
# For containers with no healthcheck (api, web): compose reporting the
# container as running is all there is to wait for. Used before exec'ing the
# CLI into the api container.
wait_running() {
  local svc="$1" timeout="${2:-120}" waited=0
  printf '    waiting for %s ' "$svc"
  until compose ps --status running --format '{{.Service}}' 2>/dev/null | grep -qx "$svc"; do
    [ "$waited" -ge "$timeout" ] && { printf '\n'; die "$svc did not start — try: development/scripts/logs.sh $svc"; }
    sleep 2; waited=$((waited + 2)); printf '.'
  done
  printf ' ok\n'
}

# wait_healthy <service> [timeout-seconds]
# Blocks until compose reports the service healthy. Both services define a
# healthcheck, so "running" is not good enough: postgres accepts TCP well
# before it accepts queries, and authentik serves a port for tens of seconds
# before the realm is importable.
wait_healthy() {
  local svc="$1" timeout="${2:-180}" waited=0 state
  printf '    waiting for %s ' "$svc"
  while :; do
    state="$(compose ps --format '{{.Health}}' "$svc" 2>/dev/null | head -1)"
    case "$state" in
      healthy) printf ' ok\n'; return 0 ;;
      unhealthy) printf '\n'; die "$svc reported unhealthy — try: development/scripts/logs.sh $svc" ;;
    esac
    [ "$waited" -ge "$timeout" ] && { printf '\n'; die "$svc not healthy after ${timeout}s — try: development/scripts/logs.sh $svc"; }
    sleep 2; waited=$((waited + 2)); printf '.'
  done
}

# wait_blueprint blocks until the identity provider is actually CONFIGURED, which
# is a later moment than it being healthy.
#
# authentik's worker applies blueprints in the background after the server starts
# answering. In that window the server is up, the hostname resolves, and every
# sign-in fails with "client not found" — which reads as a wrong client id and is
# a race. What is waited on is the thing the service itself needs: the console
# application's discovery document (design K2).
wait_blueprint() {
  local timeout="${1:-180}" waited=0
  # The issuer comes from .env, which up.sh has no other reason to have loaded.
  [ -n "${ACME_OIDC_ISSUER:-}" ] || load_env
  printf '    waiting for the authentik blueprint '
  while :; do
    if curl -fsS --max-time 3 "${ACME_OIDC_ISSUER%/}/.well-known/openid-configuration" >/dev/null 2>&1; then
      printf ' ok\n'; return 0
    fi
    [ "$waited" -ge "$timeout" ] && {
      printf '\n'
      die "the console application never appeared at ${ACME_OIDC_ISSUER} — blueprints are applied by the WORKER, so try: development/scripts/logs.sh authentik-worker"
    }
    sleep 2; waited=$((waited + 2)); printf '.'
  done
}

load_env() {
  set -a
  # shellcheck disable=SC1091
  source "$DEV_DIR/.env"
  set +a
}

# The CLI runs INSIDE the api container: that is where the Go toolchain, the
# module cache and — crucially — the container-network view of
# ACME_DATABASE_URL live. A fresh clone therefore needs Docker and nothing
# else, not even Go. `go run` is cheap here because the build cache is a
# bind mount that survives restarts.
acmectl() { compose exec -T api go run ./cmd/acmeservice "$@"; }

# Applies migrations without starting the server. The server also migrates at
# boot (ACME_MIGRATE_ON_START), but seeding happens before any server runs.
migrate_up() {
  wait_running api
  say "applying migrations"
  if ! acmectl migrate up; then
    die "migrations failed — if 'migrate' is not a known subcommand yet, the CLI (task group 20) has not landed"
  fi
}
