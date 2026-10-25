-- +goose Up
-- Центр уведомлений (УВ-2). Уведомление создаёт фоновая задача по состоянию данных; ключ
-- dedupe_key не даёт создать одно и то же уведомление пользователю дважды.
create table notifications (
    id         uuid primary key default uuidv7(),
    user_id    uuid not null references users (id) on delete cascade,
    kind       text not null,
    title      text not null,
    body       text not null default '',
    link       text,
    dedupe_key text not null,
    created_at timestamptz not null default now(),
    read_at    timestamptz,
    unique (user_id, dedupe_key)
);

create index notifications_user_idx on notifications (user_id, created_at desc);

-- +goose StatementBegin
create function notify_notification() returns trigger language plpgsql as $$
begin
    perform pikni_event('notification', new.id, array['user:' || new.user_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger notifications_events after insert on notifications
    for each row execute function notify_notification();

-- Отчёты, сформированные в фоне (ОТ-3): файл хранится семь дней в «Моих отчётах».
create table report_files (
    id          uuid primary key default uuidv7(),
    user_id     uuid not null references users (id) on delete cascade,
    report      text not null,
    title       text not null,
    params      jsonb not null default '{}',
    format      text not null check (format in ('xlsx', 'pdf')),
    status      text not null default 'queued' check (status in ('queued', 'running', 'done', 'failed')),
    error       text,
    data        bytea,
    created_at  timestamptz not null default now(),
    finished_at timestamptz,
    expires_at  timestamptz not null default now() + interval '7 days'
);

create index report_files_user_idx on report_files (user_id, created_at desc);

insert into app_settings (key, value) values ('ttn_reminder_hours', '24') on conflict do nothing;
