# Развёртывание стенда

Сервер и домен ещё не арендованы, поэтому всё ниже — готовая заготовка: проверена сборка образов и
запуск на локальной машине, но не на сервере.

## Что нужно

- Сервер с Ubuntu 24.04, 2 ядра, 4 ГБ памяти, 40 ГБ диска; Docker и плагин Compose.
- Домен, A-запись которого указывает на сервер. Без домена стенд работает на `https://localhost`
  с самоподписанным сертификатом.

## Первый запуск

```sh
git clone https://github.com/ADEXITUM/pikni.git /opt/pikni && cd /opt/pikni/deploy
cp .env.example .env    # заполнить пароли и PIKNI_DOMAIN
docker build -f Dockerfile.api -t ghcr.io/adexitum/pikni-api:latest --build-arg VERSION=$(git describe --tags --always) ..
docker build -f Dockerfile.web -t ghcr.io/adexitum/pikni-web:latest ..
docker compose -f compose.prod.yaml --env-file .env up -d postgres
docker exec -i pikni-postgres-1 psql -U postgres -c "create role govsim login password '$GOVSIM_PASSWORD'" -c "create database govsim owner govsim"
docker compose -f compose.prod.yaml --env-file .env up -d
docker compose -f compose.prod.yaml --env-file .env run --rm --entrypoint /usr/local/bin/pikni-admin api create-admin -login admin -name "Администратор"
```

Временный пароль администратора выводит последняя команда. При первом входе система попросит
сменить пароль и подключить двухфакторную аутентификацию.

## Обновление

`.github/workflows/deploy.yml` — ручной запуск: сборка образов, публикация в GHCR и `docker compose
pull && up -d` на сервере по SSH. Нужны секреты `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY`;
пока их нет, задание выключено. Миграции применяет контейнер `migrate` перед запуском API.
Обновление выполняется в сервисное окно 23:30–06:00 по Благовещенску.

## Резервные копии

`backup.sh` — по cron раз в сутки; копии хранятся 14 дней в `PIKNI_BACKUP_DIR`. Восстановление —
`restore.sh <файл>`; на локальной проверке база демо-размера восстанавливается за секунды, норматив —
не дольше 4 часов (п. 4.4.9). Копии стоит дополнительно отправлять за пределы сервера (S3-совместимое
хранилище) — это пункт на будущее.

## Наблюдаемость

Grafana на `127.0.0.1:3000` (через SSH-туннель). Дашборд «Пикни» (папка «Пикни») и четыре
правила оповещений лежат в `grafana/` и подключаются при старте. Канал оповещений (Telegram или
почта) задаётся в Grafana после развёртывания.

## Демонстрационный стенд

`demo-reset.sh` пересоздаёт базу и генерирует год работы сети по вчерашний день (около 10 минут,
API на это время останавливается). Запуск по cron ночью, например в 00:30 по Благовещенску:

    30 15 * * * /opt/pikni/deploy/demo-reset.sh >> /var/log/pikni-demo.log 2>&1

Только для демонстрационного стенда: скрипт удаляет базу `pikni`.
