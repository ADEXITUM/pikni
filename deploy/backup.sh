#!/bin/sh
# Резервная копия базы «Пикни» (п. 4.4.9): pg_dump в сжатом формате в каталог копий, запись в
# журнал backups (её видит страница «Состояние системы» и оповещение «нет копии за сутки»),
# хранение 14 дней. Запуск по cron в сервисное окно, например в 02:00 по Благовещенску:
#   0 17 * * * /opt/pikni/deploy/backup.sh >> /var/log/pikni-backup.log 2>&1
# PIKNI_PG — контейнер PostgreSQL (по умолчанию pikni-postgres-1 из compose.prod.yaml).
set -eu

PG=${PIKNI_PG:-pikni-postgres-1}
DIR=${PIKNI_BACKUP_DIR:-/var/backups/pikni}
KEEP_DAYS=${PIKNI_BACKUP_KEEP_DAYS:-14}
NAME="pikni-$(date -u +%Y%m%d-%H%M).dump"
PSQL="docker exec -i $PG psql -U postgres -d pikni -qAt -v ON_ERROR_STOP=1"

mkdir -p "$DIR"
ID=$($PSQL -c "insert into backups (file) values ('$NAME') returning id")

if docker exec "$PG" pg_dump -U postgres -d pikni -Fc -Z 6 > "$DIR/$NAME.part"; then
  mv "$DIR/$NAME.part" "$DIR/$NAME"
  SIZE=$(wc -c < "$DIR/$NAME" | tr -d ' ')
  $PSQL -c "update backups set ok = true, finished_at = now(), size_bytes = $SIZE where id = $ID" > /dev/null
  echo "backup ok: $DIR/$NAME ($SIZE bytes)"
else
  rm -f "$DIR/$NAME.part"
  $PSQL -c "update backups set error = 'pg_dump failed', finished_at = now() where id = $ID" > /dev/null
  echo "backup FAILED" >&2
  exit 1
fi

# Имитатор ЕГАИС — отдельная база рядом.
docker exec "$PG" pg_dump -U postgres -d govsim -Fc -Z 6 > "$DIR/govsim-$(date -u +%Y%m%d-%H%M).dump" || true

find "$DIR" -name '*.dump' -mtime +"$KEEP_DAYS" -delete
