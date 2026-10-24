-- +goose Up
-- Учёт (СО-1, СО-7, СО-8): документы движения и их сканы, неизменяемый регистр движений,
-- остатки по зонам как кеш суммы движений, средняя себестоимость по организации,
-- помарочный учёт и история каждой марки. Первые виды документов — ввод начальных остатков
-- и передача в торговый зал; остальные добавят следующие итерации.

-- Нумерация без пропусков: счётчик увеличивается в транзакции документа.
create table doc_counters (
    prefix  text not null,
    year    integer not null,
    last_no integer not null,
    primary key (prefix, year)
);

create table documents (
    id           uuid primary key default uuidv7(),
    kind         text not null check (kind in ('opening', 'floor_transfer')),
    number       text not null unique,
    status       text not null default 'draft'
                 check (status in ('draft', 'awaiting_egais', 'posted', 'egais_rejected', 'cancelled')),
    -- местная дата точки на момент создания
    doc_date     date not null,
    location_id  uuid not null references locations (id),
    zone_id      uuid not null references zones (id),
    dest_zone_id uuid references zones (id),
    author_id    uuid not null references users (id),
    posted_by    uuid references users (id),
    posted_at    timestamptz,
    comment      text not null default '' check (length(comment) <= 1000),
    version      integer not null default 1,
    created_at   timestamptz not null default now(),
    -- передача идёт между зонами одной точки, у остальных видов зона одна
    check ((kind = 'floor_transfer') = (dest_zone_id is not null)),
    check (dest_zone_id is distinct from zone_id)
);

-- Ввод начальных остатков — один на зону: это первая ревизия, повторный ввод исказил бы учёт.
create unique index documents_opening_zone on documents (zone_id)
    where kind = 'opening' and status <> 'cancelled';
create index documents_location_idx on documents (location_id, created_at desc);

create table document_lines (
    id          uuid primary key default uuidv7(),
    document_id uuid not null references documents (id),
    line_no     integer not null,
    variant_id  uuid not null references variants (id),
    qty         integer not null check (qty >= 0),
    -- себестоимость единицы, по которой строка проведена
    unit_cost   numeric(14, 4),
    unique (document_id, variant_id),
    unique (document_id, line_no)
);

-- Сканы документа. id присваивает устройство: повтор запроса после обрыва связи не задвоит скан.
create table scans (
    id          uuid primary key,
    document_id uuid not null references documents (id),
    user_id     uuid not null references users (id),
    source      text not null check (source in ('camera', 'wedge', 'manual')),
    raw         text not null check (length(raw) <= 300),
    code_kind   text,
    variant_id  uuid references variants (id),
    mark_code   text,
    crpt_code   text,
    qty         integer not null default 1 check (qty > 0),
    -- selected — штрихкод товара с марками: он выбран, дальше сканируются его марки; в количество не входит
    status      text not null check (status in ('accepted', 'selected', 'unknown', 'foreign', 'duplicate', 'rejected')),
    reason      text,
    scanned_at  timestamptz not null,
    received_at timestamptz not null default now(),
    undone_at   timestamptz
);

create index scans_document_idx on scans (document_id, received_at);
-- одна марка и один код «Честного знака» засчитываются в документе один раз
create unique index scans_document_mark on scans (document_id, mark_code)
    where mark_code is not null and status = 'accepted' and undone_at is null;
create unique index scans_document_crpt on scans (document_id, crpt_code)
    where crpt_code is not null and status = 'accepted' and undone_at is null;

-- Остаток — кеш суммы движений по варианту и зоне. Ограничения — последний рубеж:
-- даже ошибка в сервисе не даст отрицательного остатка.
create table stock_balances (
    variant_id  uuid not null references variants (id),
    zone_id     uuid not null references zones (id),
    location_id uuid not null references locations (id),
    on_hand     integer not null default 0 check (on_hand >= 0),
    reserved    integer not null default 0 check (reserved >= 0),
    in_transit  integer not null default 0 check (in_transit >= 0),
    -- продано через кассу сверх учёта и ещё не разобрано (решение 0013)
    deficit     integer not null default 0 check (deficit >= 0),
    updated_at  timestamptz not null default now(),
    primary key (variant_id, zone_id),
    check (reserved <= on_hand)
);

create index stock_balances_location_idx on stock_balances (location_id, variant_id);

create table stock_moves (
    id          bigint generated always as identity primary key,
    document_id uuid not null references documents (id),
    line_id     uuid references document_lines (id),
    variant_id  uuid not null references variants (id),
    location_id uuid not null references locations (id),
    zone_id     uuid not null references zones (id),
    bucket      text not null check (bucket in ('on_hand', 'in_transit', 'deficit')),
    qty         integer not null check (qty <> 0),
    cost_amount numeric(14, 2) not null,
    doc_date    date not null,
    posted_at   timestamptz not null default now()
);

create index stock_moves_variant_zone_idx on stock_moves (variant_id, zone_id, id);
create index stock_moves_document_idx on stock_moves (document_id);

-- Средняя взвешенная себестоимость по организации (ФСБУ 5/2019).
create table variant_costs (
    variant_id uuid primary key references variants (id),
    qty_total  integer not null,
    avg_cost   numeric(14, 4) not null check (avg_cost >= 0),
    updated_at timestamptz not null default now()
);

-- Марка ЕГАИС: где находится и в каком статусе. code — нормализованный текст марки.
create table excise_marks (
    code             text primary key check (code ~ '^[0-9A-Z]{14,150}$'),
    variant_id       uuid not null references variants (id),
    location_id      uuid not null references locations (id),
    zone_id          uuid references zones (id),
    status           text not null
                     check (status in ('in_stock', 'in_transit', 'reserved', 'sold', 'written_off', 'returned')),
    last_document_id uuid references documents (id),
    updated_at       timestamptz not null default now(),
    check ((status in ('in_stock', 'reserved')) = (zone_id is not null))
);

create index excise_marks_zone_idx on excise_marks (zone_id, variant_id) where zone_id is not null;

create table mark_events (
    id           bigint generated always as identity primary key,
    mark_code    text not null references excise_marks (code),
    at           timestamptz not null default now(),
    document_id  uuid references documents (id),
    user_id      uuid references users (id),
    from_status  text,
    to_status    text not null,
    from_zone_id uuid references zones (id),
    to_zone_id   uuid references zones (id)
);

create index mark_events_mark_idx on mark_events (mark_code, id);

-- Исходящие и входящие сообщения ЕГАИС (transactional outbox): сообщение пишется в транзакции
-- документа, отправляет его фоновый обработчик. Адаптер к имитатору ЕГАИС подключается в итерации 5.
create table egais_messages (
    id          uuid primary key default uuidv7(),
    location_id uuid not null references locations (id),
    direction   text not null check (direction in ('out', 'in')),
    doc_type    text not null,
    document_id uuid references documents (id),
    payload     jsonb not null,
    status      text not null default 'queued'
                check (status in ('queued', 'sent', 'accepted', 'rejected', 'error', 'processed')),
    ticket      jsonb,
    attempts    integer not null default 0,
    last_error  text,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);

create index egais_messages_queued_idx on egais_messages (created_at) where status = 'queued';
create index egais_messages_document_idx on egais_messages (document_id);

-- Регистр и история марок только пополняются: отмена документа — сторно, а не правка.
create trigger stock_moves_append_only
    before update or delete on stock_moves
    for each row execute function forbid_change();
create trigger stock_moves_no_truncate
    before truncate on stock_moves
    for each statement execute function forbid_change();
create trigger mark_events_append_only
    before update or delete on mark_events
    for each row execute function forbid_change();
create trigger mark_events_no_truncate
    before truncate on mark_events
    for each statement execute function forbid_change();

revoke update, delete on stock_moves, mark_events from pikni_app;

-- Проверка целостности (СО-7): остатки совпадают с суммой движений, а помарочный остаток —
-- с числом марок на месте. Пустой результат — расхождений нет.
-- +goose StatementBegin
create function stock_integrity()
returns table (variant_id uuid, zone_id uuid, problem text, expected bigint, actual bigint)
language sql stable as $$
    with moves as (
        select m.variant_id, m.zone_id,
               sum(m.qty) filter (where m.bucket = 'on_hand')    as on_hand,
               sum(m.qty) filter (where m.bucket = 'in_transit') as in_transit,
               sum(m.qty) filter (where m.bucket = 'deficit')    as deficit
        from stock_moves m
        group by m.variant_id, m.zone_id
    ),
    pairs as (
        select coalesce(b.variant_id, m.variant_id) as variant_id,
               coalesce(b.zone_id, m.zone_id) as zone_id,
               coalesce(b.on_hand, 0) as b_on_hand, coalesce(m.on_hand, 0) as m_on_hand,
               coalesce(b.in_transit, 0) as b_in_transit, coalesce(m.in_transit, 0) as m_in_transit,
               coalesce(b.deficit, 0) as b_deficit, coalesce(m.deficit, 0) as m_deficit
        from stock_balances b
        full join moves m on m.variant_id = b.variant_id and m.zone_id = b.zone_id
    ),
    marked as (
        select b.variant_id, b.zone_id, b.on_hand,
               (select count(*) from excise_marks e
                where e.variant_id = b.variant_id and e.zone_id = b.zone_id) as marks
        from stock_balances b
        join variants v on v.id = b.variant_id
        join products p on p.id = v.product_id
        join product_kinds k on k.code = p.kind_code
        where k.egais_accounting = 'marked'
    )
    select variant_id, zone_id, 'on_hand', m_on_hand, b_on_hand from pairs where b_on_hand <> m_on_hand
    union all
    select variant_id, zone_id, 'in_transit', m_in_transit, b_in_transit from pairs where b_in_transit <> m_in_transit
    union all
    select variant_id, zone_id, 'deficit', m_deficit, b_deficit from pairs where b_deficit <> m_deficit
    union all
    select variant_id, zone_id, 'marks', on_hand, marks from marked where marks <> on_hand
$$;
-- +goose StatementEnd

