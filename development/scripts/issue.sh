#!/usr/bin/env bash
# Force a real DNS-01 issuance through the fake-CA rig, from scratch, and show what came back.
#
# TWO certificates and two protocols, from one Traefik and one API key:
#
#   demo.example.com + *.demo.example.com   lego's httpreq  →  /present, /cleanup
#   alt.example.com                          acme-dns        →  /update
#
# The second half is acme-dns-protocol task 6.1, and the reason both run in one process is that
# the claim under test is not "acme-dns works" but that the two dialects are one implementation.
# A translation that had quietly grown logic of its own would still pass a test of either alone.
#
# traefik-lb orders a certificate when it starts and then keeps it in data/traefik_lb/acme*.json
# for ~90 days, so restarting it proves nothing on its own: it finds a valid certificate and
# logs "Testing certificate renew..." without talking to anyone. Throwing those files away is what
# makes the next start a genuine order — account registration, DNS-01 challenge, a publish into
# our service, propagation check, certificate.
#
# Everything it exercises is the real path: unmodified Traefik, real lego, real ACME against
# pebble, and our own challenge API answering on our own DNS. Note what the acme-dns half does
# NOT do: it never withdraws, because its protocol has no withdrawal (design N4).
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# True once the certificate offered for $1 is one pebble issued.
issued_by_pebble() {
  echo | openssl s_client -connect 127.0.0.1:443 -servername "$1" 2>/dev/null |
    openssl x509 -noout -issuer 2>/dev/null | grep -qi pebble
}

require_docker
acme_running || die "the acme profile is not running — ./scripts/dev.sh up --acme"

grep -q '^HTTPREQ_PASSWORD=.\+' "$DEV_DIR/$KEY_ENV" 2>/dev/null \
  || die "no API key in $KEY_ENV — run ./scripts/dev.sh seed first, or /present will answer 401"

# The acme-dns half needs its own credentials, and their absence fails in a way
# that reads like a broken endpoint rather than an unseeded stack: lego calls
# POST /register for a domain it cannot find in the storage file, and this
# service refuses registration on purpose (design N3). So check for the account
# rather than for the file, which lib.sh creates empty.
grep -q '"alt.example.com"' "$DEV_DIR/$ACMEDNS_STORE" 2>/dev/null \
  || die "no acme-dns account in $ACMEDNS_STORE — run ./scripts/dev.sh seed first, or lego will try to POST /register and be refused"

say "discarding the stored certificates (data/traefik_lb/acme*.json)"
# BOTH resolvers' stores. acme.json is the httpreq one and acme-acmedns.json the
# acme-dns one; leaving either behind would make that half of the run prove
# nothing, because Traefik finds a valid certificate and talks to nobody.
#
# acmedns.json is NOT touched: that is the registration, not a certificate, and
# deleting it would send lego to the /register this service refuses.
#
# Written by Traefik as root inside the container; removed the same way, so nobody has to sudo.
docker run --rm -v "$DEV_DIR/data/traefik_lb:/d" alpine:3 rm -f /d/acme.json /d/acme-acmedns.json

say "restarting traefik-lb — it orders on start"
compose_all restart traefik-lb >/dev/null

# Wait on the SERVED certificate, not on acme.json existing: Traefik writes that file as soon
# as it registers an ACME account, well before any certificate is in it, and it answers TLS the
# whole time with its own self-signed "TRAEFIK DEFAULT CERT". Polling the file therefore reports
# success while the order is still in flight. The issuer on the wire is the thing that changes
# exactly when the issuance completes.
say "waiting for both certificates — httpreq for demo.example.com, acme-dns for alt.example.com"
deadline=$((SECONDS + 180))
for name in demo.example.com alt.example.com; do
  until issued_by_pebble "$name"; do
    [ "$SECONDS" -ge "$deadline" ] && die "no certificate for $name after 180s — ./scripts/dev.sh logs traefik-lb"
    sleep 2; printf '.'
  done
  printf '\n'
  say "$name issued"
done

say "what our challenge API was asked to do, in both dialects"
# fqdn_form says which: `alias` and `challenge` are lego's httpreq spellings,
# `acme-dns` is the other protocol. A run that shows only one of them proves
# only half of what this script claims.
compose_all logs --since 5m api 2>/dev/null |
  grep -E 'challenge_publish|challenge_withdraw' |
  sed -E 's/.*"msg":"([a-z_]+)".*"fqdn":"([^"]+)".*"fqdn_form":"([^"]+)".*"outcome":"([^"]+)".*/  \1  \2  [\3]  \4/' || true

if ! compose_all logs --since 5m api 2>/dev/null | grep -q '"fqdn_form":"acme-dns"'; then
  die "nothing published through the acme-dns dialect — the second resolver did not run, or was refused"
fi

for name in demo.example.com alt.example.com; do
  say "the certificate now being served on https://$name (:443)"
  echo | openssl s_client -connect 127.0.0.1:443 -servername "$name" 2>/dev/null |
    openssl x509 -noout -issuer -enddate -ext subjectAltName 2>/dev/null | sed 's/^/  /'
done

say "fetching the site through it, verifying against pebble's own chain"
ca="$(mktemp)"
curl -sk https://127.0.0.1:15000/roots/0 > "$ca"
curl -sk https://127.0.0.1:15000/intermediates/0 >> "$ca"
ok=1
for name in demo.example.com alt.example.com; do
  curl -sf --cacert "$ca" --resolve "$name:443:127.0.0.1" "https://$name/" | sed "s/^/  $name  /" | head -2 || ok=0
done
if [ "$ok" != 1 ]; then
  rm -f "$ca"
  die "a certificate did not verify — ./scripts/dev.sh logs traefik-lb"
fi
rm -f "$ca"
say "issuance verified end to end, in both dialects"

# --- a SECOND, unrelated client -----------------------------------------------
#
# Everything above is lego: Traefik embeds it, so httpreq and acme-dns both went
# through one library, and a mistake in how we read the acme-dns protocol could
# be a mistake lego happens to share. acme.sh is a shell script that speaks the
# same protocol with curl and shares nothing with lego — including one thing
# nobody else checks: it GREPS THE RESPONSE BODY for the quoted value and fails
# if it is absent. A server answering 200 with an empty body passes every other
# client on the list and breaks this one.
#
# It runs as a throwaway container on the stack's own network, so this needs no
# Kubernetes and no host installation (acme-dns-protocol task 6.2). cert-manager
# is the one client that genuinely cannot be exercised without a cluster; its
# solver reads the same credentials file as lego's, which is what the traefik
# half above proves.
say "and again with acme.sh — a different implementation, over the same /update"
# shellcheck disable=SC1090
. "$DEV_DIR/$KEY_ENV"
subdomain="$(sed -n 's/.*"subdomain": "\([^"]*\)".*/\1/p' "$DEV_DIR/$ACMEDNS_STORE" | head -n1)"
[ -n "$subdomain" ] || die "no subdomain in $ACMEDNS_STORE"

if docker run --rm --network acmeservice-dev --dns 172.28.0.53 \
    -e ACMEDNS_BASE_URL="http://api:8080" \
    -e ACMEDNS_USERNAME="$HTTPREQ_USERNAME" \
    -e ACMEDNS_PASSWORD="$HTTPREQ_PASSWORD" \
    -e ACMEDNS_SUBDOMAIN="$subdomain" \
    neilpang/acme.sh --issue --dns dns_acmedns -d alt.example.com \
    --server https://pebble:14000/dir --insecure --dnssleep 5 --force 2>&1 |
    grep -E "Cert success|error|Error" | sed 's/^/  /'; then
  say "acme.sh issued too — two independent clients, one endpoint"
else
  die "acme.sh could not issue — ./scripts/dev.sh logs api"
fi
