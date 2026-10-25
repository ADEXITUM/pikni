-- +goose Up
-- Двухфакторная аутентификация по TOTP (ПБ-3): обязательна администратору, остальным — по желанию.
-- totp_pending — секрет, который ещё не подтверждён кодом из приложения.
alter table users
    add column totp_secret  text,
    add column totp_pending text,
    add column totp_enabled boolean not null default false;

-- Резервные коды на случай потери телефона: одноразовые, хранятся хешем.
create table totp_recovery_codes (
    user_id   uuid not null references users (id) on delete cascade,
    code_hash bytea not null,
    used_at   timestamptz,
    primary key (user_id, code_hash)
);

-- Вход в два шага: после пароля — короткоживущий вызов, сеанс создаётся после кода.
create table login_challenges (
    token_hash bytea primary key,
    user_id    uuid not null references users (id) on delete cascade,
    attempts   integer not null default 0,
    expires_at timestamptz not null
);

-- Вход на смартфоне по QR-коду с ПК (ПБ-2): одноразовый код на 60 секунд.
create table qr_logins (
    code_hash  bytea primary key,
    user_id    uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    used_at    timestamptz
);

-- Ключи API для внешних систем (ИН-2): ограниченный набор прав, журнал обращений, отзыв.
create table api_keys (
    id           uuid primary key default uuidv7(),
    name         text not null check (length(btrim(name)) between 1 and 100),
    prefix       text not null unique,
    key_hash     bytea not null,
    scopes       text[] not null check (scopes <@ array['availability', 'catalog', 'import'] and cardinality(scopes) > 0),
    created_by   uuid not null references users (id),
    created_at   timestamptz not null default now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

create table api_key_requests (
    id        bigint generated always as identity primary key,
    key_id    uuid not null references api_keys (id),
    at        timestamptz not null default now(),
    method    text not null,
    operation text not null,
    ip        inet,
    ok        boolean not null
);

create index api_key_requests_key_idx on api_key_requests (key_id, id desc);

revoke update, delete on api_key_requests from pikni_app;

-- Журнал резервных копий: страница «Состояние системы» и оповещение «нет копии за сутки».
create table backups (
    id          bigint generated always as identity primary key,
    started_at  timestamptz not null default now(),
    finished_at timestamptz,
    file        text not null,
    size_bytes  bigint,
    ok          boolean not null default false,
    error       text
);
