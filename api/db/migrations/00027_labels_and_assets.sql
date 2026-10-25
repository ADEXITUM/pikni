-- +goose Up
-- Напечатанные ценники точки: цена на момент печати. Ценник устарел, если текущая розничная
-- цена от неё отличается (ЭТ-3).
create table price_tag_prints (
    location_id uuid not null references locations (id),
    variant_id  uuid not null references variants (id),
    price       numeric(12, 2) not null,
    printed_at  timestamptz not null default now(),
    printed_by  uuid references users (id),
    primary key (location_id, variant_id)
);

-- Учёт имущества сети (ИМ-1): оборудование и мебель точек, не товар.
create table asset_categories (
    code text primary key,
    name text not null unique
);

insert into asset_categories (code, name) values
    ('refrigeration', 'Холодильное оборудование'),
    ('displays', 'Витрины и стеллажи'),
    ('pos', 'Кассовое оборудование'),
    ('computers', 'Вычислительная техника'),
    ('furniture', 'Мебель');

create sequence asset_number_seq;

-- Инвентарный номер INV-000001 распознаёт сканер (СК-4); номер не переиспользуется.
create table assets (
    id             uuid primary key default uuidv7(),
    number         text not null unique check (number ~ '^INV-[0-9]{6}$'),
    name           text not null check (length(btrim(name)) between 1 and 200),
    category_code  text not null references asset_categories (code),
    serial         text not null default '' check (length(serial) <= 100),
    accepted_at    date not null,
    cost           numeric(14, 2) not null check (cost >= 0),
    location_id    uuid not null references locations (id),
    responsible_id uuid references users (id),
    status         text not null default 'in_use' check (status in ('in_use', 'repair', 'written_off')),
    photo          bytea,
    photo_type     text check (photo_type in ('image/jpeg', 'image/png', 'image/webp')),
    version        integer not null default 1,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now()
);

create index assets_location_idx on assets (location_id, status);
create index assets_responsible_idx on assets (responsible_id) where status <> 'written_off';

-- История объекта (ИМ-3): перемещения, смена МОЛ и состояния — только добавление.
create table asset_events (
    id               bigint generated always as identity primary key,
    asset_id         uuid not null references assets (id),
    kind             text not null check (kind in ('created', 'moved', 'responsible', 'status',
                     'writeoff_requested', 'writeoff_rejected', 'written_off', 'found_elsewhere')),
    from_location_id uuid references locations (id),
    to_location_id   uuid references locations (id),
    from_user_id     uuid references users (id),
    to_user_id       uuid references users (id),
    from_status      text,
    to_status        text,
    comment          text not null default '',
    user_id          uuid references users (id),
    at               timestamptz not null default now()
);

create index asset_events_asset_idx on asset_events (asset_id, id desc);

-- Списание имущества «двумя ключами» (ИМ-5, ПБ-8).
create table asset_writeoffs (
    id               uuid primary key default uuidv7(),
    asset_id         uuid not null references assets (id),
    reason           text not null check (length(btrim(reason)) between 1 and 1000),
    status           text not null default 'pending' check (status in ('pending', 'approved', 'rejected')),
    requested_by     uuid not null references users (id),
    requested_at     timestamptz not null default now(),
    decided_by       uuid references users (id),
    decided_at       timestamptz,
    decision_comment text not null default ''
);

create unique index asset_writeoffs_pending_idx on asset_writeoffs (asset_id) where status = 'pending';

-- Инвентаризация имущества (ИМ-4): сканы наклеек в точке; результат считается по ним.
create table asset_audits (
    id          uuid primary key default uuidv7(),
    location_id uuid not null references locations (id),
    status      text not null default 'counting' check (status in ('counting', 'finished', 'cancelled')),
    started_by  uuid not null references users (id),
    started_at  timestamptz not null default now(),
    finished_by uuid references users (id),
    finished_at timestamptz
);

create unique index asset_audits_open_idx on asset_audits (location_id) where status = 'counting';

create table asset_audit_scans (
    audit_id   uuid not null references asset_audits (id),
    asset_id   uuid not null references assets (id),
    scanned_by uuid not null references users (id),
    scanned_at timestamptz not null default now(),
    primary key (audit_id, asset_id)
);

revoke update, delete on asset_events from pikni_app;

