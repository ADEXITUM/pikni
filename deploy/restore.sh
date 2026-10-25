#!/bin/sh
# Восстановление из резервной копии (П-34): ./restore.sh /var/backups/pikni/pikni-20261024-1700.dump
# Останавливает API, пересоздаёт базу из копии, применяет миграции новой версии и запускает API.
# Роли pikni_owner и pikni_app создаёт initdb при первом запуске контейнера PostgreSQL.
set -eu

FILE=${1:?usage: restore.sh <file.dump>}
PG=${PIKNI_PG:-pikni-postgres-1}
COMPOSE="docker compose -f $(dirname "$0")/compose.prod.yaml --env-file $(dirname "$0")/.env"
START=$(date +%s)

$COMPOSE stop api api-2
docker exec -i "$PG" psql -U postgres -v ON_ERROR_STOP=1 -c "drop database if exists pikni with (force)" -c "create database pikni owner pikni_owner"
docker exec -i "$PG" pg_restore -U postgres -d pikni --no-owner --role=pikni_owner < "$FILE"
docker exec -i "$PG" psql -U postgres -d pikni -v ON_ERROR_STOP=1 \
  -c "grant select, insert, update, delete on all tables in schema public to pikni_app" \
  -c "grant usage, select on all sequences in schema public to pikni_app" \
  -c "revoke update, delete on audit_log, stock_moves, mark_events, asset_events, api_key_requests from pikni_app"
$COMPOSE run --rm migrate
$COMPOSE start api api-2

echo "restored $FILE in $(( $(date +%s) - START )) s"
