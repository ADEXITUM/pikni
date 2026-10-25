-- +goose Up
-- Списание (СО-5) и сторно проведённого документа с утверждением «двумя ключами» (ПБ-8),
-- брони покупателей (НЛ-4, НЛ-5).

alter table documents drop constraint documents_kind_check;
alter table documents add constraint documents_kind_check
    check (kind in ('opening', 'floor_transfer', 'receipt', 'sale', 'transfer', 'writeoff', 'storno'));

alter table documents drop constraint documents_status_check;
alter table documents add constraint documents_status_check
    check (status in ('requested', 'rejected', 'draft', 'pending_approval', 'in_transit', 'awaiting_egais', 'posted',
                      'egais_rejected', 'cancelled', 'reversed'));

alter table documents
    add column reason_id   uuid references writeoff_reasons (id),
    add column reverses_id uuid references documents (id);

alter table documents add constraint documents_writeoff_check check ((kind = 'writeoff') = (reason_id is not null));
alter table documents add constraint documents_storno_check check ((kind = 'storno') = (reverses_id is not null));
-- Сторно одно на документ.
create unique index documents_reverses_key on documents (reverses_id) where reverses_id is not null and status <> 'cancelled';

-- Для боя нужна фотография — подтверждение для руководителя и проверяющих.
alter table writeoff_reasons add column photo_required boolean not null default false;
update writeoff_reasons set photo_required = true where name = 'Бой';

-- Решения по утверждению: кто утвердил или вернул документ и почему (ПБ-8).
create table document_approvals (
    id          bigint generated always as identity primary key,
    document_id uuid not null references documents (id),
    user_id     uuid not null references users (id),
    decision    text not null check (decision in ('approved', 'rejected')),
    comment     text not null default '' check (length(comment) <= 1000),
    created_at  timestamptz not null default now()
);

create index document_approvals_document_idx on document_approvals (document_id);

-- Фотографии к документу (бой при списании). Хранятся в базе: их немного и они небольшие —
-- клиент уменьшает снимок до 1600 px.
create table document_photos (
    id           uuid primary key default uuidv7(),
    document_id  uuid not null references documents (id),
    content_type text not null check (content_type in ('image/jpeg', 'image/png', 'image/webp')),
    data         bytea not null check (octet_length(data) <= 3 * 1024 * 1024),
    created_by   uuid not null references users (id),
    created_at   timestamptz not null default now()
);

create index document_photos_document_idx on document_photos (document_id);

create table reservations (
    id             uuid primary key default uuidv7(),
    number         text not null unique,
    location_id    uuid not null references locations (id),
    variant_id     uuid not null references variants (id),
    qty            integer not null check (qty > 0),
    customer_name  text not null check (length(customer_name) between 1 and 200),
    customer_phone text not null check (length(customer_phone) <= 20),
    -- без согласия покупателя на обработку персональных данных бронь не оформляется
    consent        boolean not null check (consent),
    status         text not null default 'new'
                   check (status in ('new', 'confirmed', 'issued', 'cancelled', 'expired')),
    -- срок — конец следующего дня по местному времени точки
    expires_at     timestamptz not null,
    zone_id        uuid references zones (id),
    created_by     uuid not null references users (id),
    confirmed_by   uuid references users (id),
    receipt_id     uuid references pos_receipts (id),
    comment        text not null default '' check (length(comment) <= 500),
    created_at     timestamptz not null default now(),
    closed_at      timestamptz,
    anonymized_at  timestamptz,
    check ((status = 'confirmed') <= (zone_id is not null))
);

create index reservations_open_idx on reservations (location_id, status) where status in ('new', 'confirmed');
create index reservations_expiry_idx on reservations (expires_at) where status in ('new', 'confirmed');

-- Отложенные бутылки с марками: бронь подтверждается сканом марки каждой.
create table reservation_marks (
    reservation_id uuid not null references reservations (id),
    mark_code      text not null references excise_marks (code),
    primary key (reservation_id, mark_code)
);

-- +goose StatementBegin
create function notify_reservation() returns trigger language plpgsql as $$
begin
    perform pikni_event('reservation', new.id, array['location:' || new.location_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger reservations_events after insert or update on reservations
    for each row execute function notify_reservation();
