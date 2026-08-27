#!/usr/bin/env bash
# Seeds a fresh clone up to a populated console: a sample project, load balancer,
# nodes, a wildcard domain and an API key, through `acmeservice adminctl` —
# the same code path an operator uses, so a seed that works proves the CLI
# works. Keycloak needs no seeding here: its realm, four groups and four test
# users are imported from realm/ when its data directory is empty.
#
# Idempotent: re-running against an already-seeded database does nothing.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_docker
load_env

compose ps --status running --format '{{.Service}}' 2>/dev/null | grep -qx postgres \
  || die "postgres is not running — development/scripts/up.sh"
wait_healthy postgres 60

# resolve_cname <name> — what the fake internet answers for name, asked from
# the HOST through CoreDNS's published port. dig when it is installed (the
# documented check, `dig @127.0.0.1 -p 8053 <name> CNAME +short`), otherwise
# busybox inside the alpine image the other scripts already use, so a machine
# with nothing but Docker still works.
resolve_cname() {
  if command -v dig >/dev/null 2>&1; then
    dig @127.0.0.1 -p 8053 "$1" CNAME +short
  else
    docker run --rm --network "container:$(compose ps -q coredns)" alpine:3 \
      nslookup -type=cname "$1" 127.0.0.1 2>/dev/null | sed -n 's/.*canonical name = //p'
  fi
}

# Publishes the customer side of the delegation: the _acme-challenge CNAMEs
# that development/dns/zones/example.com.zone serves, taken from the alias the
# service ACTUALLY created rather than from a label guessed in advance. With
# the shipped default (ACME_ALIAS_USE_DOMAIN unset) that label is random, so
# this is the only way the fake internet can point at it.
publish_delegation() {
  say "publishing the challenge delegation into the fake internet's example.com"
  local plan lines deadline name target line
  plan="$(acmectl adminctl loadbalancer dns demo/prod)" || die "could not render the DNS plan"

  # ONE CNAME PER ALIAS BY CONSTRUCTION: the plan emits exactly one
  # _acme-challenge row per distinct alias — demo.example.com and
  # *.demo.example.com share one — so copying its rows can never double up,
  # and a seed that grew a second alias would get a second CNAME for free.
  # Only the zone-snippet half of the output is read; the table above it
  # prints the same records with the columns in another order.
  mapfile -t lines < <(printf '%s\n' "$plan" | sed -n '/Zone snippet:/,$p' |
    awk '$1 ~ /^_acme-challenge\./ && $4 == "CNAME" { print $1 "  IN CNAME  " $5 }')
  [ "${#lines[@]}" -gt 0 ] || die "the DNS plan contains no _acme-challenge CNAME:
$plan"

  render_zone "${lines[@]}"
  printf '%s\n' "${lines[@]}" | sed 's/^/    /'

  # CoreDNS's `file` plugin re-reads a changed zone on a poll timer, so
  # writing the file and carrying on would race every caller. Restart, then
  # wait for the answer rather than for the container.
  say "restarting coredns and waiting for it to answer"
  compose restart coredns >/dev/null
  deadline=$((SECONDS + 60))
  for line in "${lines[@]}"; do
    name="${line%% *}"; target="${line##* }"
    until resolve_cname "$name" | grep -qx "$target"; do
      [ "$SECONDS" -ge "$deadline" ] && die "$name does not resolve to $target through 127.0.0.1:8053"
      sleep 1; printf '.'
    done
  done
  say "the fake internet now delegates to ${lines[0]##* }"
}

# --- guard: never seed twice -------------------------------------------------
existing="$(compose exec -T postgres psql -qtAX -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "select count(*) from projects" 2>/dev/null | tr -d '[:space:]' || true)"
if [ -z "$existing" ]; then
  migrate_up
  existing=0
fi
if [ "$existing" != "0" ]; then
  # The served zone is generated, not checked in, so an already-seeded database
  # can still be paired with an empty delegation — a fresh clone of the
  # repository against a surviving data/ directory, say. Republishing is
  # derived entirely from the database, so it is safe to redo.
  grep -q 'IN CNAME' "$DEV_DIR/$ZONE_FILE" || publish_delegation
  say "already seeded ($existing project(s)) — nothing to do"
  say "start over with: development/scripts/reset.sh"
  exit 0
fi

# --- inventory ---------------------------------------------------------------
say "seeding inventory"
acmectl adminctl project create --slug demo --name "Demo Project"
acmectl adminctl loadbalancer create --project demo --slug prod --name "Production"
acmectl adminctl node add --loadbalancer demo/prod --label a --ipv4 203.0.113.10
acmectl adminctl node add --loadbalancer demo/prod --label b --ipv4 203.0.113.11
acmectl adminctl domain create --loadbalancer demo/prod --name "demo.example.com"
acmectl adminctl domain create --loadbalancer demo/prod --name "*.demo.example.com"
# demo.example.com and *.demo.example.com deliberately share one alias and one
# CNAME — the wildcard case the whole design turns on. `adminctl loadbalancer dns`
# below should therefore show TWO domains but only ONE _acme-challenge record.

publish_delegation

# --- api key, captured for traefik-lb ---------------------------------------
# The secret is displayed exactly ONCE and is never stored, so the only chance
# to hand it to the `acme` profile's load balancer is right here. Basic auth:
# the key identifier is the username, the secret is the password.
say "minting an API key"
key_out="$(acmectl adminctl key create --loadbalancer demo/prod --name "demo-prod")"
printf '%s\n' "$key_out"

key_id="$(printf '%s\n' "$key_out" | sed -n '1s/.*created api key "\([^"]*\)".*/\1/p')"
key_secret="$(printf '%s\n' "$key_out" | tail -n1 | tr -d '[:space:]')"
[ -n "$key_id" ] && [ -n "$key_secret" ] \
  || die "could not parse the key out of \`adminctl key create\`; the secret is unrecoverable — reset.sh and try again"

umask 077
cat >"$DEV_DIR/$KEY_ENV" <<EOF
# Written by development/scripts/seed.sh; read by traefik-lb as an env_file.
# The secret cannot be recovered from the database — only revoked and
# replaced. Wiped, with the rest of data/, by destroy.sh.
HTTPREQ_USERNAME=$key_id
HTTPREQ_PASSWORD=$key_secret
EOF
say "wrote $KEY_ENV for traefik-lb ($key_id)"

# An already-running traefik-lb started with the empty placeholder and would
# keep being refused 401 until it is restarted with the real values.
if acme_running; then
  say "restarting traefik-lb with the new key"
  compose_all up -d --force-recreate traefik-lb
fi

# --- identity provider -------------------------------------------------------
say "verifying the identity provider"
if curl -fsS --max-time 5 "${ACME_OIDC_ISSUER%/}/.well-known/openid-configuration" >/dev/null 2>&1; then
  say "console application ready at ${ACME_OIDC_ISSUER}"
else
  warn "the console application is not answering at ${ACME_OIDC_ISSUER}"
  warn "blueprints are applied by the WORKER, not the server, so look there first:"
  warn "  development/scripts/logs.sh authentik-worker"
fi

echo
say "generated DNS plan for demo/prod:"
acmectl adminctl loadbalancer dns demo/prod || warn "could not render the DNS plan"

echo
say "seeded — sign in at http://acme-console.localhost as admin-user / devpass"
