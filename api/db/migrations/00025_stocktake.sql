-- +goose Up
-- Ревизия (СО-6): учётный остаток фиксируется при старте подсчёта, товары ревизии в точке
-- блокируются, зоны считаются параллельно, марки сличаются помарочно. Корректировка остатков —
-- после утверждения «двумя ключами» (ПБ-8), в ЕГАИС — акты постановки на баланс и списания.

alter table documents drop constraint documents_kind_check;
alter table documents add constraint documents_kind_check
    check (kind in ('opening', 'floor_transfer', 'receipt', 'sale', 'transfer', 'writeoff', 'storno', 'stocktake'));

alter table documents drop constraint documents_status_check;
alter table documents add constraint documents_status_check
    check (status in ('requested', 'rejected', 'draft', 'counting', 'reconciling', 'pending_approval', 'in_transit',
                      'awaiting_egais', 'posted', 'egais_rejected', 'cancelled', 'reversed'));

-- Состав выборочной ревизии: зоны, категории (с подкатегориями), бренды. null — вся точка.
alter table documents add column scope jsonb;

-- Скан ревизии относится к зоне, которую в этот момент считают.
alter table scans add column zone_id uuid references zones (id);

create table stocktake_zones (
    document_id uuid not null references documents (id),
    zone_id     uuid not null references zones (id),
    status      text not null default 'counting' check (status in ('counting', 'closed')),
    closed_by   uuid references users (id),
    closed_at   timestamptz,
    primary key (document_id, zone_id)
);

-- Учётный остаток на момент старта: количество и средняя себестоимость.
create table stocktake_book (
    document_id uuid not null references documents (id),
    zone_id     uuid not null references zones (id),
    variant_id  uuid not null references variants (id),
    qty         integer not null,
    cost_e4     bigint not null default 0,
    primary key (document_id, zone_id, variant_id)
);

create table stocktake_book_marks (
    document_id uuid not null references documents (id),
    zone_id     uuid not null references zones (id),
    mark_code   text not null references excise_marks (code),
    variant_id  uuid not null references variants (id),
    primary key (document_id, mark_code)
);

-- Подсчёт товара без марок: сканы прибавляют по одному, количество можно задать вручную.
create table stocktake_counts (
    document_id uuid not null references documents (id),
    zone_id     uuid not null references zones (id),
    variant_id  uuid not null references variants (id),
    qty         integer not null check (qty >= 0),
    primary key (document_id, zone_id, variant_id)
);

-- Найденные марки: своя этой зоны, своя из другой зоны точки или чужая (нет в учёте точки).
create table stocktake_marks (
    document_id uuid not null references documents (id),
    zone_id     uuid not null references zones (id),
    mark_code   text not null,
    variant_id  uuid references variants (id),
    kind        text not null check (kind in ('found', 'moved', 'foreign')),
    primary key (document_id, mark_code)
);

-- stocktake_blocks — товар входит в идущую ревизию точки: движения по нему, кроме самой ревизии,
-- запрещены до её завершения.
-- +goose StatementBegin
create function stocktake_blocks(p_location uuid, p_zone uuid, p_variant uuid, p_except uuid)
returns text language sql stable as $$
    select d.number
    from documents d
    join variants v on v.id = p_variant
    join products p on p.id = v.product_id
    where d.kind = 'stocktake' and d.location_id = p_location and d.id is distinct from p_except
      and d.status in ('counting', 'reconciling', 'pending_approval')
      and (d.scope is null or (
            (d.scope -> 'zones' is null or jsonb_array_length(d.scope -> 'zones') = 0 or d.scope -> 'zones' ? p_zone::text)
        and (d.scope -> 'brands' is null or jsonb_array_length(d.scope -> 'brands') = 0 or d.scope -> 'brands' ? coalesce(p.brand_id::text, ''))
        and (d.scope -> 'categories' is null or jsonb_array_length(d.scope -> 'categories') = 0 or exists (
              with recursive up as (
                  select c.id, c.parent_id from categories c where c.id = p.category_id
                  union all
                  select c.id, c.parent_id from categories c join up on c.id = up.parent_id)
              select 1 from up where d.scope -> 'categories' ? up.id::text))))
    limit 1;
$$;
-- +goose StatementEnd

insert into app_settings (key, value) values ('organization_name', 'ООО «Чекушка»') on conflict do nothing;
