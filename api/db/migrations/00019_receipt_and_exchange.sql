-- +goose Up
-- Поступление по ТТН ЕГАИС в четырёх режимах приёмки (СО-2) и обмен с ЕГАИС через УТМ (ИН-3).

alter table documents drop constraint documents_kind_check;
alter table documents add constraint documents_kind_check
    check (kind in ('opening', 'floor_transfer', 'receipt'));
alter table documents
    add column supplier_id    uuid references suppliers (id),
    add column receiving_mode text check (receiving_mode in ('document', 'boxes', 'sample', 'piece')),
    add column ttn_id         text,
    add column ttn_number     text,
    add column ttn_date       date,
    -- позиции ТТН, которых нет в каталоге: приёмку нельзя завершить, пока их не сопоставят
    add column unmapped       jsonb not null default '[]';
alter table documents add constraint documents_receipt_check
    check ((kind = 'receipt') = (receiving_mode is not null));

-- Одна ТТН — один документ поступления в точке: повторное получение сообщения его не задвоит.
create unique index documents_ttn on documents (location_id, ttn_id) where ttn_id is not null;

alter table document_lines
    add column qty_plan     integer check (qty_plan >= 0),
    add column price        numeric(12, 2) check (price >= 0),
    add column ttn_identity integer;

-- Марки, перечисленные в ТТН: с ними сверяется скан. Марка не из этого списка — «чужая».
create table ttn_marks (
    document_id  uuid not null references documents (id),
    code         text not null,
    variant_id   uuid not null references variants (id),
    package_code text,
    primary key (document_id, code)
);

create index ttn_marks_package_idx on ttn_marks (document_id, package_code);

-- Короба ТТН. to_open — короб, который при выборочной приёмке надо вскрыть и сосчитать поштучно.
create table document_packages (
    document_id uuid not null references documents (id),
    code        text not null,
    variant_id  uuid references variants (id),
    qty         integer not null check (qty >= 0),
    to_open     boolean not null default false,
    primary key (document_id, code)
);

alter table scans add column package_code text;
create unique index scans_document_package on scans (document_id, package_code)
    where package_code is not null and status = 'accepted' and undone_at is null;

alter table egais_messages
    add column external_id     text,
    add column next_attempt_at timestamptz not null default now();
-- Входящее сообщение обрабатывается один раз, даже если опрос УТМ вернул его повторно.
create unique index egais_messages_external on egais_messages (location_id, direction, external_id)
    where external_id is not null;
drop index egais_messages_queued_idx;
create index egais_messages_queued_idx on egais_messages (next_attempt_at) where status = 'queued';

-- Ответы ЕГАИС на запрос остатков — основа сверки (СО-9, итерация 9).
create table egais_rests_snapshots (
    id          uuid primary key default uuidv7(),
    location_id uuid not null references locations (id),
    taken_at    timestamptz not null default now(),
    payload     jsonb not null
);

-- Поступление создаёт сама система, когда из ЕГАИС приходит ТТН: автора-сотрудника у него нет.
alter table documents alter column author_id drop not null;
