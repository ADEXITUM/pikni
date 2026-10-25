#!/bin/sh
# Демонстрационный стенд (п. 4.1.4): данные восстанавливаются каждую ночь. База пересоздаётся,
# получает схему, справочники и каталог, затем генератор пишет год работы сети по вчерашний день —
# так на защите отчёты и дашборд всегда показывают свежий год. Занимает около 10 минут.
# Запуск по cron в сервисное окно, например в 00:30 по Благовещенску:
#   30 15 * * * /opt/pikni/deploy/demo-reset.sh >> /var/log/pikni-demo.log 2>&1
set -eu

cd "$(dirname "$0")"
PG=${PIKNI_PG:-pikni-postgres-1}
COMPOSE="docker compose -f compose.prod.yaml --env-file .env"
PSQL="docker exec -i $PG psql -q -v ON_ERROR_STOP=1 -U postgres"
START=$(date +%s)

$COMPOSE stop api api-2
$PSQL -c "drop database if exists pikni with (force)" -c "create database pikni owner pikni_owner"
$PSQL -d pikni \
  -c "alter default privileges for role pikni_owner in schema public grant select, insert, update, delete on tables to pikni_app" \
  -c "alter default privileges for role pikni_owner in schema public grant usage, select on sequences to pikni_app"
$COMPOSE run --rm migrate
docker exec -i "$PG" psql -q -v ON_ERROR_STOP=1 -U pikni_owner -d pikni -f /seed/dev.sql
$COMPOSE run --rm --entrypoint /usr/local/bin/pikni-admin api import-catalog -file /seed/catalog.csv
docker exec -i "$PG" psql -q -v ON_ERROR_STOP=1 -U pikni_owner -d pikni -f /seed/matrix.sql
$COMPOSE run --rm --entrypoint /usr/local/bin/pikni-admin api seed-demo-year -days 365
$COMPOSE start api api-2

echo "demo data restored in $(( $(date +%s) - START )) s"
