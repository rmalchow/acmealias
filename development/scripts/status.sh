#!/usr/bin/env bash
# What is running, whether it is usable, and everything needed to sign in.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_docker
load_env

echo
printf '\033[1mcontainers\033[0m\n'
if compose_all ps -q 2>/dev/null | grep -q .; then
  printf '  %-10s %-9s %-10s %s\n' SERVICE STATE HEALTH PORTS
  compose_all ps --format '{{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}' 2>/dev/null |
    while IFS=$'\t' read -r svc state health ports; do
      printf '  %-10s %-9s %-10s %s\n' "$svc" "$state" "${health:--}" "$ports"
    done
else
  echo "  (stack is not running — ./scripts/dev.sh up)"
fi

echo
printf '\033[1murls\033[0m\n'
printf '  console (customer)         %s\n' "http://console.localhost"
printf '  console (staff)            %s\n' "http://admin.localhost"
printf '  admin API (same origin)    %s\n' "http://console.localhost/api/v1"
printf '  admin API (own hostname)   %s\n' "http://acme-api.localhost/api/v1   # curl/adminctl, not the browser session"
printf '  challenge API              %s\n' "http://acme-challenge.localhost   # lego httpreq; API-key auth"
printf '  identity provider          %s\n' "http://auth.localhost   # admin UI at /if/admin/"
printf '  marketing site             %s\n' "http://www.localhost"
printf '  mail (everything sent)     %s\n' "http://mail.localhost   # mailpit; senders use mailpit:1025 over SMTP"
printf '  postgres                   %s\n' "localhost:5432/${POSTGRES_DB}"
printf '  (:80 and :443 are bound on ALL interfaces — a second machine on the\n'
printf '   LAN can reach them, given the hostnames in its own /etc/hosts)\n'

echo
printf '\033[1msign-in\033[0m\n'
printf '  %-22s %s\n' USER GROUP
printf '  %-22s %s\n' admin@example.com  "acme-admin        (the staff console)"
printf '  %-22s %s\n' user@example.com   "acme-read-only"
printf '  the four the acceptance suite uses:\n'
printf '  %-22s %s\n' admin-user         "acme-admin"
printf '  %-22s %s\n' config-admin-user  "acme-config-admin"
printf '  %-22s %s\n' key-rotation-user  "acme-key-rotation"
printf '  %-22s %s\n' read-only-user     "acme-read-only"
printf '  all passwords: devpass       authentik admin: akadmin / %s\n' "$AUTHENTIK_BOOTSTRAP_PASSWORD"
printf '  groups grant NO roles (design C10). What a person may do depends on which\n'
printf '  accounts they are a member of; acme-admin only gates http://admin.localhost.\n'

echo
printf '\033[1mdata\033[0m\n'
if compose_all ps --status running --format '{{.Service}}' 2>/dev/null | grep -qx postgres; then
  counts="$(compose exec -T postgres psql -qtAX -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c \
    "select 'projects='||(select count(*) from projects)||' nodes='||(select count(*) from lb_nodes)||' domains='||(select count(*) from domains)||' aliases='||(select count(*) from aliases)||' api_keys='||(select count(*) from api_keys)" 2>/dev/null || true)"
  if [ -n "$counts" ]; then
    printf '  %s\n' "$counts"
    case "$counts" in *"projects=0"*) printf '  not seeded yet — ./scripts/dev.sh seed\n' ;; esac
  else
    printf '  schema not migrated yet — ./scripts/dev.sh up\n'
  fi
else
  printf '  postgres not running\n'
fi

echo
printf '\033[1mdns (fake internet)\033[0m\n'
printf '  coredns is authoritative for example.com, forwards dev.acmeservice.internal\n'
printf '  to the api container and everything else upstream. From the host:\n'
printf '    dig @127.0.0.1 -p 8053 _acme-challenge.demo.example.com CNAME +short\n'
printf '    dig @127.0.0.1 -p 8153 dev.acmeservice.internal SOA +short   # api, directly\n'

echo
printf '\033[1macme profile\033[0m\n'
if acme_running; then
  printf '  active — pebble (fake CA), traefik-lb (customer-like), whoami\n'
  if grep -q '^HTTPREQ_PASSWORD=.\+' "$DEV_DIR/$KEY_ENV" 2>/dev/null; then
    printf '  api key: %s\n' "$(sed -n 's/^HTTPREQ_USERNAME=//p' "$DEV_DIR/$KEY_ENV")"
  else
    printf '  \033[1;33mno api key yet\033[0m — traefik-lb will be refused 401 by /present\n'
    printf '  run: ./scripts/dev.sh seed\n'
  fi
  printf '  drive an issuance end to end:\n'
  printf '    ./scripts/dev.sh logs traefik-lb      # it orders on start; watch it\n'
  printf '    ./scripts/dev.sh logs api             # challenge_publish, then _withdraw\n'
  printf '    curl -sk https://127.0.0.1:15000/roots/0 > /tmp/pebble-ca.pem\n'
  printf '    curl -sk https://127.0.0.1:15000/intermediates/0 >> /tmp/pebble-ca.pem\n'
  printf '    curl -s --cacert /tmp/pebble-ca.pem \\\n'
  printf '      --resolve demo.example.com:443:127.0.0.1 https://demo.example.com/\n'
  printf '  (--resolve because the fake example.com zone lives in the stack CoreDNS,\n'
  printf '   which the host does not use; the chain needs BOTH pebble files, the\n'
  printf '   root and the intermediate it actually signs leaves with. Neither is\n'
  printf '   data/pebble/minica.pem — that one only signs pebble own API cert.)\n'
else
  printf '  not running — ./scripts/dev.sh up --acme\n'
fi

echo
printf '\033[1mlive reload\033[0m\n'
printf '  api  air, in-container, watching ../backend   frontend  ng serve, watching ../frontend\n'
printf '  logs ./scripts/dev.sh logs api        (./scripts/dev.sh watch follows both)\n'
echo
