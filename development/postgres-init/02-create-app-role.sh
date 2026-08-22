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

for db in "$POSTGRES_DB" acmeservice_test; do
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$db" <<-SQL
		GRANT ALL ON DATABASE $db TO ${ACME_DB_ROLE:-acmeservice_app};
		ALTER SCHEMA public OWNER TO ${ACME_DB_ROLE:-acmeservice_app};
		GRANT ALL ON SCHEMA public TO ${ACME_DB_ROLE:-acmeservice_app};
	SQL
done
