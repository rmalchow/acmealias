#!/usr/bin/env bash
# Force a real DNS-01 issuance through the fake-CA rig, from scratch, and show what came back.
#
# traefik-lb orders a certificate when it starts and then keeps it in data/traefik_lb/acme.json
# for ~90 days, so restarting it proves nothing on its own: it finds a valid certificate and
# logs "Testing certificate renew..." without talking to anyone. Throwing that file away is what
# makes the next start a genuine order — account registration, DNS-01 challenge, /present into
# our service, propagation check, /cleanup, certificate.
#
# Everything it exercises is the real path: unmodified Traefik, real lego, real ACME against
# pebble, and our own challenge API answering on our own DNS.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# True once the certificate offered for demo.example.com is one pebble issued.
issued_by_pebble() {
  echo | openssl s_client -connect 127.0.0.1:443 -servername demo.example.com 2>/dev/null |
    openssl x509 -noout -issuer 2>/dev/null | grep -qi pebble
}

require_docker
acme_running || die "the acme profile is not running — ./scripts/dev.sh up --acme"

grep -q '^HTTPREQ_PASSWORD=.\+' "$DEV_DIR/$KEY_ENV" 2>/dev/null \
  || die "no API key in $KEY_ENV — run ./scripts/dev.sh seed first, or /present will answer 401"

say "discarding the stored certificate (data/traefik_lb/acme.json)"
# Written by Traefik as root inside the container; removed the same way, so nobody has to sudo.
docker run --rm -v "$DEV_DIR/data/traefik_lb:/d" alpine:3 rm -f /d/acme.json

say "restarting traefik-lb — it orders on start"
compose_all restart traefik-lb >/dev/null

# Wait on the SERVED certificate, not on acme.json existing: Traefik writes that file as soon
# as it registers an ACME account, well before any certificate is in it, and it answers TLS the
# whole time with its own self-signed "TRAEFIK DEFAULT CERT". Polling the file therefore reports
# success while the order is still in flight. The issuer on the wire is the thing that changes
# exactly when the issuance completes.
say "waiting for the certificate"
deadline=$((SECONDS + 120))
until issued_by_pebble; do
  [ "$SECONDS" -ge "$deadline" ] && die "no certificate after 120s — ./scripts/dev.sh logs traefik-lb"
  sleep 2; printf '.'
done
printf '\n'

say "what our challenge API was asked to do"
compose_all logs --since 3m api 2>/dev/null |
  grep -E 'challenge_publish|challenge_withdraw' |
  sed -E 's/.*"msg":"([a-z_]+)".*"fqdn":"([^"]+)".*"outcome":"([^"]+)".*/  \1  \2  \3/' || true

say "the certificate now being served on https://demo.example.com (:443)"
echo | openssl s_client -connect 127.0.0.1:443 -servername demo.example.com 2>/dev/null |
  openssl x509 -noout -issuer -enddate -ext subjectAltName 2>/dev/null | sed 's/^/  /'

say "fetching the site through it, verifying against pebble's own chain"
ca="$(mktemp)"
curl -sk https://127.0.0.1:15000/roots/0 > "$ca"
curl -sk https://127.0.0.1:15000/intermediates/0 >> "$ca"
if curl -sf --cacert "$ca" --resolve demo.example.com:443:127.0.0.1 https://demo.example.com/ | sed 's/^/  /' | head -3; then
  say "issuance verified end to end"
else
  rm -f "$ca"
  die "the certificate did not verify — ./scripts/dev.sh logs traefik-lb"
fi
rm -f "$ca"
