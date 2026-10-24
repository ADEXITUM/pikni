-- +goose Up
-- Учебный имитатор смежных систем (docs/design/govsim.md): ЕГАИС с УТМ точек, ГИС МТ «Честный знак»
-- и поставщик. Все марки, коды и идентификаторы вымышленные.

create table participants (
    fsrar_id text primary key check (fsrar_id ~ '^[0-9]{12}$'),
    inn      text not null default '',
    kpp      text not null default '',
    name     text not null,
    kind     text not null check (kind in ('supplier', 'retail_point', 'warehouse')),
    address  text not null default '',
    -- сценарии ИС-5: УТМ недоступен, ответы приходят с задержкой
    utm_down      boolean not null default false,
    delay_seconds integer not null default 0 check (delay_seconds between 0 and 3600)
);

-- Справочник продукции ЕГАИС: по алкокоду. Штрихкод и GTIN нужны генератору поставок для печати.
create table products (
    alcocode         text primary key check (alcocode ~ '^[0-9]{19}$'),
    name             text not null,
    kind_code        text not null,
    volume_ml        integer,
    abv              numeric(4, 1),
    producer         text not null default '',
    egais_accounting text not null check (egais_accounting in ('marked', 'quantity')),
    barcode          text,
    gtin             text,
    crpt             boolean not null default false,
    price            numeric(12, 2) not null default 0
);

create table marks (
    code        text primary key,
    alcocode    text not null references products,
    owner_fsrar text not null references participants,
    status      text not null check (status in ('in_transit', 'on_balance', 'sold', 'written_off')),
    blocked     boolean not null default false,
    package_code text,
    ttn_id      uuid,
    updated_at  timestamptz not null default now()
);

create table mark_events (
    id          bigint generated always as identity primary key,
    code        text not null references marks,
    at          timestamptz not null default now(),
    event       text not null,
    fsrar_id    text,
    document_id uuid
);

create index mark_events_code_idx on mark_events (code, id);

-- Продукция без марок учитывается количеством: регистр 1 — склад, 2 — торговый зал.
create table rests (
    fsrar_id text not null references participants,
    alcocode text not null references products,
    register smallint not null check (register in (1, 2)),
    qty      integer not null check (qty >= 0),
    primary key (fsrar_id, alcocode, register)
);

-- Журнал документов: всё, что пришло во «входящие» ЕГАИС, и всё, что создал генератор.
create table documents (
    id            uuid primary key default uuidv7(),
    fsrar_from    text not null,
    fsrar_to      text,
    type          text not null,
    payload       jsonb not null,
    status        text not null check (status in ('received', 'accepted', 'rejected')),
    reject_reason text,
    created_at    timestamptz not null default now()
);

create index documents_created_idx on documents (created_at desc);

-- «Исходящие» УТМ участника: ТТН, квитанции, ответы на запросы.
create table outbox (
    id            uuid primary key default uuidv7(),
    fsrar_id      text not null references participants,
    type          text not null,
    payload       jsonb not null,
    document_id   uuid references documents,
    created_at    timestamptz not null default now(),
    visible_after timestamptz not null default now(),
    acked_at      timestamptz
);

create index outbox_pending_idx on outbox (fsrar_id, created_at) where acked_at is null;

-- Реестр кодов «Честного знака». code — GTIN и серийный номер без кода проверки.
create table crpt_codes (
    code         text primary key,
    gtin         text not null,
    serial       text not null,
    alcocode     text references products,
    owner_fsrar  text not null references participants,
    status       text not null check (status in ('introduced', 'sold', 'blocked')),
    produced_at  date not null,
    expires_at   date not null,
    batch        text not null,
    block_reason text,
    ttn_id       uuid
);

create index crpt_codes_batch_idx on crpt_codes (batch);

-- Поставки генератора: что лежит в коробах на самом деле (с заложенными расхождениями) — для печати.
create table shipments (
    id         uuid primary key default uuidv7(),
    ttn_id     uuid not null references documents,
    supplier   text not null references participants,
    recipient  text not null references participants,
    number     text not null,
    print      jsonb not null,
    notes      text not null default '',
    created_at timestamptz not null default now()
);

create sequence ttn_number_seq;
