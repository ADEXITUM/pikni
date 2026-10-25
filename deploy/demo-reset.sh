#!/bin/sh
# Демонстрационный стенд (п. 4.1.4, решение 0063): данные восстанавливаются каждую ночь. Базы
# пересоздаются: схема, справочники, реальный каталог (catalog-full.csv), год работы сети по
# вчерашний день, учётные записи всех ролей, остатки точек в имитаторе ЕГАИС и все сценарии —
# на защите отчёты показывают свежий год, а ревизия, приёмка и перемещения ждут продолжения.
# Занимает около полутора часов.
# Запуск по cron в сервисное окно, например в 00:30 по Благовещенску:
#   30 15 * * * /opt/pikni/deploy/demo-reset.sh >> /var/log/pikni-demo.log 2>&1
set -eu

cd "$(dirname "$0")"
PG=${PIKNI_PG:-pikni-postgres-1}
COMPOSE="docker compose -f compose.prod.yaml --env-file .env"
PSQL="docker exec -i $PG psql -q -v ON_ERROR_STOP=1 -U postgres"
OUT="$(pwd)/.demo-out"
ADMIN="$COMPOSE run --rm -v $OUT:/out -v $(pwd)/../api/db/seed:/seed:ro --entrypoint /usr/local/bin/pikni-admin api"
GOVSIM="$COMPOSE run --rm -v $OUT:/out -v $(pwd)/../api/db/seed:/seed:ro --entrypoint /usr/local/bin/govsim govsim"
START=$(date +%s)
mkdir -p "$OUT"

$COMPOSE stop api api-2 govsim
$PSQL -c "drop database if exists pikni with (force)" -c "create database pikni owner pikni_owner" \
  -c "drop database if exists govsim with (force)" -c "create database govsim owner govsim"
$PSQL -d pikni \
  -c "alter default privileges for role pikni_owner in schema public grant select, insert, update, delete on tables to pikni_app" \
  -c "alter default privileges for role pikni_owner in schema public grant usage, select on sequences to pikni_app"
$COMPOSE run --rm migrate
docker exec -i "$PG" psql -q -v ON_ERROR_STOP=1 -U pikni_owner -d pikni -f /seed/dev.sql
$ADMIN import-catalog -file /seed/catalog-full.csv
$ADMIN seed-demo-year -days 365 -rests /out/rests.json
$ADMIN seed-users
$GOVSIM migrate
$GOVSIM seed -catalog /seed/catalog-full.csv
$GOVSIM seed-rests -file /out/rests.json
$COMPOSE start govsim
$ADMIN scenario all
$COMPOSE start api api-2

echo "demo data restored in $(( $(date +%s) - START )) s"
