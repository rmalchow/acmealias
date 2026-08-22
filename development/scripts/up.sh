#!/usr/bin/env bash
# Creates and starts the dev stack, waits until it is actually usable, and
# applies migrations. Idempotent: safe to run against an already-running stack.
#
# Everything runs in containers, including the Go binary (air) and the Angular
# dev server (ng serve) — both live-reload off the bind-mounted source.
#
#   up.sh            postgres, keycloak, coredns, api, web, dev traefik
#   up.sh --acme     the above plus the `acme` profile: pebble (a fake CA),
#                    a customer-like traefik-lb and a whoami behind it, so a
#                    real DNS-01 issuance can be driven end to end.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

want_acme=0
case "${1:-}" in
  --acme) want_acme=1 ;;
  "")     ;;
  *)      die "usage: up.sh [--acme]" ;;
esac

require_docker

# Leaving the profile out of a plain up.sh would otherwise tear the acme
# containers down as orphans — surprising, since nothing asked for that.
if [ "$want_acme" = 0 ] && acme_running; then
  say "acme profile is already running — keeping it up"
  want_acme=1
fi

make_data_dirs

if [ "$want_acme" = 1 ]; then
  enable_acme_profile
  # traefik-lb has to trust the fake CA's API before it can order anything.
  # The certificate is inside the pebble image, so copy it out rather than
  # checking a throwaway CA cert into the repository.
  if [ ! -s "$DEV_DIR/data/pebble/minica.pem" ]; then
    say "extracting pebble's CA certificate"
    cid="$(docker create ghcr.io/letsencrypt/pebble:latest)"
    docker cp "$cid:/test/certs/pebble.minica.pem" "$DEV_DIR/data/pebble/minica.pem" \
      || die "could not extract /test/certs/pebble.minica.pem from the pebble image"
    docker rm -f "$cid" >/dev/null
  fi
fi

say "starting containers"
compose up -d --remove-orphans

wait_healthy postgres
wait_healthy keycloak

migrate_up

say "dev stack up"
"$DEV_DIR/scripts/status.sh"
