#!/bin/sh
# Первичная настройка кластера: выполняется образом postgres один раз, при создании пустого тома.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v db="$POSTGRES_DB" \
  -v owner_password="${PIKNI_OWNER_PASSWORD:?}" \
  -v app_password="${PIKNI_APP_PASSWORD:?}" <<'SQL'
create role pikni_owner login password :'owner_password';
create role pikni_app login password :'app_password';

alter database :"db" owner to pikni_owner;

alter default privileges for role pikni_owner in schema public
    grant select, insert, update, delete on tables to pikni_app;
alter default privileges for role pikni_owner in schema public
    grant usage, select on sequences to pikni_app;
SQL
