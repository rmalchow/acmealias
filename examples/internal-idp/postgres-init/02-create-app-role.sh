#!/bin/sh
# Runs once, against a fresh data directory.
#
# The service must NOT connect as a superuser. PostgreSQL exempts superusers
# from every row-level security policy, silently and unconditionally, so a
# deployment that connects that way has no tenant isolation at all while looking
# entirely healthy — and the default role in this image is a superuser. The
# binary refuses to start as one (store.CheckTenantIsolation); this creates the
# role it should use instead.
#
# The role OWNS the schema, so migrations can create tables. That is safe on its
# own: an owner bypasses its tables' policies only when they are not FORCEd, and
# migration 000007 forces every one of them.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-SQL
	CREATE ROLE ${ACME_DB_ROLE:-acmeservice_app} LOGIN
	  PASSWORD '${ACME_DB_PASSWORD:-acmeservice_app}'
	  NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
SQL

# PRODUCTION HAS NO TEST DATABASE. The development copy of this script loops
# over $POSTGRES_DB and acmeservice_test, the second created by
# 01-create-test-db.sh — which is deliberately not deployed here, because a test
# database in production is a second place for real data to end up.
#
# That mismatch is not a harmless leftover: with ON_ERROR_STOP the missing
# database fails this script, and docker-entrypoint ABANDONS THE WHOLE
# init sequence at the first failure — so 03-create-authentik-db.sh never ran,
# authentik had no role to log in as, and the symptom was
# "password authentication failed" for a role that did not exist.
for db in "$POSTGRES_DB"; do
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$db" <<-SQL
		GRANT ALL ON DATABASE $db TO ${ACME_DB_ROLE:-acmeservice_app};
		ALTER SCHEMA public OWNER TO ${ACME_DB_ROLE:-acmeservice_app};
		GRANT ALL ON SCHEMA public TO ${ACME_DB_ROLE:-acmeservice_app};
	SQL
done
