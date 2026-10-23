-- +goose Up
create table users (
    id                   uuid primary key default uuidv7(),
    login                text not null,
    full_name            text not null,
    role                 text not null check (role in (
                             'admin', 'manager', 'accountant',
                             'store_manager', 'storekeeper', 'cashier')),
    password_hash        text not null,
    must_change_password boolean not null default true,
    is_blocked           boolean not null default false,
    failed_logins        integer not null default 0,
    locked_until         timestamptz,
    version              integer not null default 1,
    created_at           timestamptz not null default now(),
    updated_at           timestamptz not null default now()
);

create unique index users_login_key on users (lower(login));

create table user_locations (
    user_id     uuid not null references users (id) on delete cascade,
    location_id uuid not null references locations (id),
    primary key (user_id, location_id)
);

create index user_locations_location_id_idx on user_locations (location_id);

create table sessions (
    id           uuid primary key default uuidv7(),
    user_id      uuid not null references users (id) on delete cascade,
    token_hash   bytea not null unique,
    csrf_token   text not null,
    ip           inet,
    user_agent   text,
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null
);

create index sessions_user_id_idx on sessions (user_id);

-- session_id без внешнего ключа: сессии удаляются при выходе, а журнал хранится вечно.
create table audit_log (
    id          bigint generated always as identity primary key,
    at          timestamptz not null default now(),
    user_id     uuid references users (id),
    session_id  uuid,
    ip          inet,
    action      text not null,
    entity_type text,
    entity_id   text,
    data        jsonb not null default '{}'
);

create index audit_log_at_idx on audit_log (at);
create index audit_log_user_id_at_idx on audit_log (user_id, at);
create index audit_log_entity_idx on audit_log (entity_type, entity_id);

-- +goose StatementBegin
create function forbid_change() returns trigger
language plpgsql as $$
begin
    raise exception 'table % is append-only', tg_table_name;
end;
$$;
-- +goose StatementEnd

create trigger audit_log_append_only
    before update or delete on audit_log
    for each row execute function forbid_change();

create trigger audit_log_no_truncate
    before truncate on audit_log
    for each statement execute function forbid_change();

revoke update, delete on audit_log from pikni_app;
