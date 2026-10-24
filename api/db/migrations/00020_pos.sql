-- +goose Up
-- Учебная касса (КС-1 – КС-7): зарегистрированные кассы с ключом, вход кассира по PIN, смены,
-- чеки продажи и возврата. Каждая смена ведёт документ «Продажа»: чеки сразу списывают остаток зала.

-- PIN кассира (4–6 цифр) хранится хешем, как пароль.
alter table users add column pin_hash text;

create table pos_devices (
    id           uuid primary key default uuidv7(),
    location_id  uuid not null references locations (id),
    name         text not null check (length(name) between 1 and 100),
    -- ключ кассы: pos_<prefix>_<secret>; префикс виден в списке касс, секрет хранится хешем
    key_prefix   text not null unique,
    key_hash     bytea not null,
    created_by   uuid references users (id),
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz,
    revoked_at   timestamptz
);

create table pos_sessions (
    id           uuid primary key default uuidv7(),
    device_id    uuid not null references pos_devices (id),
    user_id      uuid not null references users (id),
    token_hash   bytea not null unique,
    created_at   timestamptz not null default now(),
    expires_at   timestamptz not null,
    revoked_at   timestamptz
);

alter table documents drop constraint documents_kind_check;
alter table documents add constraint documents_kind_check
    check (kind in ('opening', 'floor_transfer', 'receipt', 'sale'));

create table pos_shifts (
    id               uuid primary key default uuidv7(),
    device_id        uuid not null references pos_devices (id),
    cashier_id       uuid not null references users (id),
    number           integer not null,
    sale_document_id uuid not null references documents (id),
    opened_at        timestamptz not null default now(),
    closed_at        timestamptz,
    status           text not null default 'open' check (status in ('open', 'closed', 'auto_closed')),
    unique (device_id, number)
);

-- На кассе открыта не больше одной смены.
create unique index pos_shifts_open on pos_shifts (device_id) where status = 'open';

create table pos_receipts (
    -- идентификатор задаёт касса: повторная отправка чека не создаёт дубль
    id                    uuid primary key,
    shift_id              uuid not null references pos_shifts (id),
    number                integer not null,
    kind                  text not null check (kind in ('sale', 'return')),
    return_of             uuid references pos_receipts (id),
    created_on_device_at  timestamptz not null,
    received_at           timestamptz not null default now(),
    total                 numeric(14, 2) not null,
    payment               text not null check (payment in ('cash', 'card')),
    age_confirmed         boolean not null default false,
    egais_qr              text,
    offline               boolean not null default false,
    unique (shift_id, number)
);

create table pos_receipt_lines (
    receipt_id uuid not null references pos_receipts (id),
    line_no    integer not null,
    variant_id uuid not null references variants (id),
    qty        integer not null check (qty <> 0),
    price      numeric(12, 2) not null check (price >= 0),
    mark_code  text,
    crpt_code  text,
    primary key (receipt_id, line_no)
);

-- Повторную продажу марки не допускает статус марки (excise_marks): чек блокирует её строку.
-- Уникальный индекс по строкам не подходит — после возврата марку можно продать снова.
create index pos_receipt_lines_mark_idx on pos_receipt_lines (mark_code) where mark_code is not null;
