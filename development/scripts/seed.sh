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

# zone_name — the zone this service is authoritative for, without its trailing
# dot. Every challenge alias is a label inside it, so it is the second half of
# every target published below.
#
# ACME_ZONE_NAME wins, exactly as it does for the service; otherwise it is the
# one line of development/config-development.yaml that names it. That file's own
# header says seed.sh cannot read YAML, and this is not a reader: it is the one
# key this script cannot do without, taken with a `die` behind it so a moved
# key fails here rather than three steps later as an NXDOMAIN.
zone_name() {
  local z="${ACME_ZONE_NAME:-}"
  [ -n "$z" ] || z="$(awk '
    /^zone:/ { in_zone = 1; next }
    /^[^[:space:]#]/ { in_zone = 0 }
    in_zone && $1 == "name:" { print $2; exit }' "$DEV_DIR/config-development.yaml")"
  z="${z%.}"
  [ -n "$z" ] || die "no zone name: set ACME_ZONE_NAME, or restore zone.name in development/config-development.yaml"
  printf '%s\n' "$z"
}

# delegation_targets — the `_acme-challenge` CNAMEs the demo project's domains
# need published, one line per distinct alias, in the shape render_zone takes.
#
# This used to shell `adminctl project dns demo` and split its output on the
# literal `Zone snippet:`. That subcommand is gone (domain-diagnostics 11.6):
# what a domain needs published is a property of the DOMAIN now, served as
# `cnameTarget` on the domain read. Nothing in a shell script can reach that
# read — every management route is behind an OIDC console session, and walking
# authentik's flow executor is what the Go acceptance suite exists for — so the
# seed takes the same two facts out of the database it is already talking to.
#
# ONE CNAME PER ALIAS BY CONSTRUCTION, which is why the select is over aliases
# and not over domains: demo.example.com and *.demo.example.com share one alias
# and therefore one row, and a seed that grew a second alias would get a second
# CNAME for free.
#
# ponytail: the target is composed here as `<label>.<zone>` rather than read
# from the one function that composes it, inventory.DelegationTarget. Ceiling:
# a change to how a target is spelled has to be made twice, and the second
# spelling is this one. Upgrade path: teach `adminctl domain list` to print the
# target — the CLI already prints it once, from cnameLine, when a domain is
# created — and this helper becomes a parse of that.
delegation_targets() {
  local zone challenge label
  zone="$(zone_name)"
  compose exec -T postgres psql -qtAX -F' ' -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c \
    "select distinct a.challenge_fqdn, a.label
       from aliases a
       join projects p on p.id = a.project_id
      where p.slug = 'demo'
      order by 1" |
    while read -r challenge label; do
      [ -n "$label" ] || continue
      printf '%s.  IN CNAME  %s.%s.\n' "${challenge%.}" "$label" "$zone"
    done
}

# Publishes the customer side of the delegation: the _acme-challenge CNAMEs
# that development/dns/zones/example.com.zone serves, taken from the alias the
# service ACTUALLY created rather than from a label guessed in advance. With
# the shipped default (ACME_ALIAS_USE_DOMAIN unset) that label is random, so
# this is the only way the fake internet can point at it.
publish_delegation() {
  say "publishing the challenge delegation into the fake internet's example.com"
  local lines deadline name target line
  mapfile -t lines < <(delegation_targets)
  [ "${#lines[@]}" -gt 0 ] \
    || die "the demo project has no alias, so there is nothing to delegate to — is it seeded?"

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

# --- the account --------------------------------------------------------------
#
# The migration no longer seeds one (design C10): every account belongs to a
# person, and the first one on a fresh deployment is made here. ACME_ADMINCTL_ACCOUNT
# in development/.env names it, and every adminctl call below acts inside it.
#
# The owner is a STAND-IN, and that is a known gap rather than a shortcut. The
# authentik provider uses sub_mode: hashed_user_id, so a person's subject is a
# hash this script cannot know until they have signed in once — there is nothing
# to name here. Personal accounts ARE materialized at sign-in now, so a developer
# who signs in lands somewhere they can work — but not in THIS account, which
# still needs an invitation nobody can send yet (account-invitations). adminctl
# can reach it, which is what the seed is for.
#
# To reach it from the CONSOLE: sign in once so the provider issues your subject,
# read it off /api/v1/me, and then hand the account over:
#
#   acmectl adminctl account transfer --owner "<subject from /me>"
#
# or, to join without taking ownership:
#
#   acmectl adminctl account member-add --subject "<subject>" --role ADMIN
say "creating the account"
# --credits is the opening balance: a team account works only while it is above
# zero, so there is no zero-credit form of this verb. 100 rather than the default
# 25 so that a developer running the stack for a week does not have to think
# about it.
acmectl adminctl account create \
  --slug "$ACME_ADMINCTL_ACCOUNT" --name "Internal" \
  --owner "system:dev-seed-owner" --credits 100

# --- inventory ---------------------------------------------------------------
say "seeding inventory"
# One object where there were two: the seed used to create the project `demo`
# and then the load balancer `prod` inside it, and everything below hung off
# `prod`. A project IS that object now.
acmectl adminctl project create --slug demo --name "Demo Project"
acmectl adminctl node add --project demo --label a --ipv4 203.0.113.10
acmectl adminctl node add --project demo --label b --ipv4 203.0.113.11
acmectl adminctl domain create --project demo --name "demo.example.com"
acmectl adminctl domain create --project demo --name "*.demo.example.com"
# A third domain, for the OTHER dialect. traefik-lb obtains one certificate for
# demo.example.com through lego's httpreq protocol and a second for this one
# through acme-dns — same Traefik, same key, same challenge API, two spellings
# (acme-dns-protocol task 6.1). It is a separate name because two routers cannot
# share a Host rule, and a separate name means a separate alias and its own
# CNAME, which publish_delegation picks up for free.
acmectl adminctl domain create --project demo --name "alt.example.com"
# demo.example.com and *.demo.example.com deliberately share one alias and one
# CNAME — the wildcard case the whole design turns on. publish_delegation below
# therefore writes TWO records for THREE domains, not three.

publish_delegation

# --- api key, captured for traefik-lb ---------------------------------------
# The secret is displayed exactly ONCE and is never stored, so the only chance
# to hand it to the `acme` profile's load balancer is right here. Basic auth:
# the key identifier is the username, the secret is the password.
say "minting an API key"
key_out="$(acmectl adminctl key create --project demo --name "demo")"
printf '%s\n' "$key_out"

# Matches on `api key "..."` rather than on the whole opening phrase. The CLI
# prints "created api key" for a system key and "created PROJECT api key" for a
# project one (api-key-kinds K1), and a pattern anchored to the first wording
# stopped matching the day the second appeared — with the failure arriving here,
# in a shell script, as an unrecoverable secret rather than in the change that
# renamed it.
key_id="$(printf '%s\n' "$key_out" | sed -n '1s/.*api key "\([^"]*\)".*/\1/p')"
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

# --- the same key, spelled the acme-dns way ----------------------------------
#
# lego's acme-dns provider looks the domain up in this file. FINDING IT IS WHAT
# MATTERS: with an account present it goes straight to /update, and with none it
# calls /register — which this service refuses (design N3), because an
# unauthenticated endpoint that mints inventory is the shape the whole tenancy
# model exists to prevent. So the file is the registration, produced here by
# something that is already authorized, exactly as the console produces it for a
# customer.
#
# The shape is goacmedns's: an object keyed by DOMAIN. `fulldomain` is the name
# the customer's CNAME points at and `subdomain` is its first label — both read
# back off the alias the service created rather than guessed, because with the
# shipped default the label is random and only the running system knows it.
say "writing the acme-dns credentials for traefik-lb"
alt_target="$(delegation_targets |
  awk '$1 == "_acme-challenge.alt.example.com." { print $NF }' | tail -n1)"
[ -n "$alt_target" ] || die "no _acme-challenge CNAME for alt.example.com — did its domain get created?"
alt_full="${alt_target%.}"          # jxet64y6bngq4j37.dev.acmeservice.internal
alt_label="${alt_full%%.*}"         # jxet64y6bngq4j37

umask 077
cat >"$DEV_DIR/$ACMEDNS_STORE" <<EOF
{
  "alt.example.com": {
    "fulldomain": "$alt_full",
    "subdomain": "$alt_label",
    "username": "$key_id",
    "password": "$key_secret",
    "server_url": ""
  }
}
EOF
say "wrote $ACMEDNS_STORE for alt.example.com ($alt_label)"

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

# --- wait for the delegation to be seen ---------------------------------------
#
# A domain is offline until the activation worker has seen its delegation
# (domain-activation A1), and the zone above is what publishes it. Without this
# wait the seed returns while demo.example.com is still refusing /present with
# 409, and traefik-lb — which orders a certificate the moment it starts — fails
# its first attempt for a reason that looks nothing like the cause.
say "waiting for the delegation to be seen"
n=0
until [ "$(compose exec -T postgres psql -qtAX -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
        -c "select count(*) from domains where last_success_at is not null" 2>/dev/null | tr -d ' ')" != "0" ]; do
  n=$((n + 2))
  [ "$n" -ge 120 ] && { warn "no domain activated after ${n}s — is ACME_VERIFY_RESOLVER set, and is the worker running?"; break; }
  sleep 2; printf '.'
done
printf ' ok\n'

echo
say "the delegation the demo project's domains need published:"
delegation_targets | sed 's/^/    /' || warn "could not read the delegation back"

echo
say "seeded — sign in at http://console.localhost as admin@example.com / devpass"
