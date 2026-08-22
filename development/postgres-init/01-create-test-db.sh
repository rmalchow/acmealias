#!/bin/sh
# Runs once, only against a fresh (empty) data directory — see
# docker-entrypoint-initdb.d in the postgres image docs. Creates the second
# database scripts/test-integration.sh points TEST_DATABASE_URL at, alongside
# the main $POSTGRES_DB the image already creates from its own env.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-SQL
	CREATE DATABASE acmeservice_test OWNER $POSTGRES_USER;
SQL
