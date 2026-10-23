-- +goose Up
-- Цены вариантов с историей (НСИ-6): новая цена — новая строка с датой начала действия,
-- прежние не меняются. Действует последняя цена, чья дата уже наступила; будущую можно
-- назначить заранее. Закупочная цена — цена по умолчанию до приёмки, её видят не все роли.
create table prices (
    id         uuid primary key default uuidv7(),
    variant_id uuid not null references variants (id),
    kind       text not null check (kind in ('retail', 'purchase')),
    price      numeric(12, 2) not null check (price > 0),
    valid_from timestamptz not null,
    set_by     uuid references users (id),
    created_at timestamptz not null default now(),
    unique (variant_id, kind, valid_from)
);

create index prices_current_idx on prices (variant_id, kind, valid_from desc);

-- МРЦ — минимальные розничные цены Минфина России. Правило задаёт цену эталонного объёма
-- (обычно 0,5 л); для другого объёма цена пересчитывается пропорционально, а если указана
-- эталонная крепость — ещё и пропорционально крепости. abv_over — правило действует только
-- для продукции крепче этого значения. Новая редакция — новая строка с датой начала действия.
create table min_retail_prices (
    id         uuid primary key default uuidv7(),
    kind_code  text not null references product_kinds (code),
    valid_from date not null,
    price      numeric(12, 2) not null check (price > 0),
    volume_ml  integer not null default 500 check (volume_ml > 0),
    abv        numeric(4, 1) check (abv > 0 and abv <= 100),
    abv_over   numeric(4, 1) check (abv_over >= 0 and abv_over < 100),
    source     text not null,
    unique (kind_code, valid_from)
);
