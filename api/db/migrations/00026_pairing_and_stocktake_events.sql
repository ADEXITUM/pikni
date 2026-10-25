-- +goose Up
-- Сопряжение смартфона с ПК (СК-6): ПК показывает QR-код, смартфон того же пользователя его
-- сканирует, дальше коды со смартфона приходят в открытый на ПК документ через события.
-- Идентификатор — случайный UUID v4: по нему подписываются на коды, угадать его нельзя.
create table pairings (
    id          uuid primary key default gen_random_uuid(),
    user_id     uuid not null references users (id),
    secret_hash bytea not null,
    status      text not null default 'waiting' check (status in ('waiting', 'active', 'closed')),
    created_at  timestamptz not null default now(),
    joined_at   timestamptz,
    closed_at   timestamptz,
    expires_at  timestamptz not null
);

create index pairings_user_idx on pairings (user_id, created_at desc);

-- Закрытие зоны и ручной подсчёт видны на экранах всей комиссии ревизии (УВ-1).
-- +goose StatementBegin
create function notify_stocktake() returns trigger language plpgsql as $$
begin
    perform pikni_event('scan', new.document_id, array['doc:' || new.document_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger stocktake_zones_events after insert or update on stocktake_zones
    for each row execute function notify_stocktake();
create trigger stocktake_counts_events after insert or update on stocktake_counts
    for each row execute function notify_stocktake();
