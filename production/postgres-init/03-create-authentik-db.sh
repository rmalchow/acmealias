#!/bin/sh
# Runs once, against a fresh data directory.
#
# authentik needs a PostgreSQL database. It does not need a PostgreSQL of its
# OWN: this stack already runs one, and a second server would be a second thing
# to start, wait for, back up and reason about on every reset — for a dev stack
# whose reset cost is already the reason an acceptance gate went unproven for a
# while (design K4).
#
# It gets its own database and its own role rather than sharing the service's.
# The service's role is deliberately NOSUPERUSER NOBYPASSRLS so row-level
# security applies to it, and lending that role to the identity provider would
# tangle two unrelated blast radii for no benefit.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-SQL
	CREATE ROLE ${AUTHENTIK_DB_ROLE:-authentik} LOGIN
	  PASSWORD '${AUTHENTIK_DB_PASSWORD:-authentik}'
	  NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
	CREATE DATABASE ${AUTHENTIK_DB_NAME:-authentik} OWNER ${AUTHENTIK_DB_ROLE:-authentik};
SQL

# authentik's migrations create everything in `public`, so the role has to own
# that schema in its own database.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "${AUTHENTIK_DB_NAME:-authentik}" <<-SQL
	ALTER SCHEMA public OWNER TO ${AUTHENTIK_DB_ROLE:-authentik};
	GRANT ALL ON SCHEMA public TO ${AUTHENTIK_DB_ROLE:-authentik};
SQL
