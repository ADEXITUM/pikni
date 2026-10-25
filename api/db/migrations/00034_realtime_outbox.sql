-- +goose Up
-- События реального времени — через таблицу, а не через NOTIFY (решение 0064). Коммиты транзакций
-- с NOTIFY PostgreSQL выполняет по одному на весь сервер: при федеральной ревизии каждый скан ждал
-- своей очереди, и сканов в секунду было не больше пятисот. Вставка в таблицу общей блокировки не
-- берёт; каждый экземпляр сервера забирает новые события раз в 200 мс. Таблица нежурналируемая:
-- события живут минуту и после сбоя не нужны — клиенты при переподключении перезапрашивают данные.
create unlogged table realtime_events (
    id         bigint generated always as identity primary key,
    payload    jsonb not null,
    created_at timestamptz not null default now()
);

create index realtime_events_created_idx on realtime_events (created_at);

grant select, insert, delete on realtime_events to pikni_app;
grant usage on sequence realtime_events_id_seq to pikni_app;

-- +goose StatementBegin
create or replace function pikni_event(kind text, id uuid, topics text[]) returns void
language sql as $$
    insert into realtime_events (payload) values (json_build_object('type', kind, 'id', id, 'topics', topics));
$$;
-- +goose StatementEnd
