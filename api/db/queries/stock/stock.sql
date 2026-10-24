-- Регистр движений и остатки (СО-7). Суммы в базе — numeric, в приложении — целые:
-- деньги в копейках, средняя себестоимость в десятитысячных долях рубля (_e4).

-- name: EnsureBalances :exec
insert into stock_balances (variant_id, zone_id, location_id)
select unnest(@variant_ids::uuid[]), unnest(@zone_ids::uuid[]), unnest(@location_ids::uuid[])
on conflict do nothing;

-- name: LockBalances :many
-- Все документы блокируют остатки в одном порядке, поэтому взаимоблокировок нет.
select b.variant_id, b.zone_id, b.on_hand, b.reserved, b.in_transit, b.deficit
from stock_balances b
join (select unnest(@variant_ids::uuid[]) as variant_id, unnest(@zone_ids::uuid[]) as zone_id) as t
  on b.variant_id = t.variant_id and b.zone_id = t.zone_id
order by b.variant_id, b.zone_id
for update of b;

-- name: ChangeBalance :exec
update stock_balances
set on_hand = on_hand + @on_hand::integer,
    in_transit = in_transit + @in_transit::integer,
    deficit = deficit + @deficit::integer,
    updated_at = now()
where variant_id = @variant_id and zone_id = @zone_id;

-- name: EnsureCosts :exec
insert into variant_costs (variant_id, qty_total, avg_cost)
select unnest(@variant_ids::uuid[]), 0, 0
on conflict do nothing;

-- name: LockCosts :many
select variant_id, qty_total, (avg_cost * 10000)::bigint as avg_cost_e4
from variant_costs
where variant_id = any(@variant_ids::uuid[])
order by variant_id
for update;

-- name: SetCost :exec
update variant_costs
set qty_total = @qty_total, avg_cost = sqlc.arg(avg_cost_e4)::bigint / 10000.0, updated_at = now()
where variant_id = @variant_id;

-- name: InsertMoves :exec
insert into stock_moves (document_id, line_id, variant_id, location_id, zone_id, bucket, qty, cost_amount, doc_date)
select @document_id::uuid, nullif(t.line_id, '00000000-0000-0000-0000-000000000000'::uuid), t.variant_id,
       t.location_id, t.zone_id, t.bucket, t.qty, t.cost_kopecks / 100.0, @doc_date::date
from (select unnest(@line_ids::uuid[]) as line_id, unnest(@variant_ids::uuid[]) as variant_id,
             unnest(@location_ids::uuid[]) as location_id, unnest(@zone_ids::uuid[]) as zone_id,
             unnest(@buckets::text[]) as bucket, unnest(@qtys::integer[]) as qty,
             unnest(@cost_kopecks::bigint[]) as cost_kopecks) as t;

-- name: ListBalances :many
-- Остатки точки страницами по (наименование, объём, вариант); поиск — как в каталоге.
select b.variant_id, v.product_id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       b.zone_id, z.kind as zone_kind, b.on_hand, b.reserved, b.in_transit, b.deficit,
       coalesce((c.avg_cost * 10000)::bigint, 0)::bigint as avg_cost_e4
from stock_balances b
join variants v on v.id = b.variant_id
join products p on p.id = v.product_id
join zones z on z.id = b.zone_id
left join variant_costs c on c.variant_id = b.variant_id
where b.location_id = @location_id
  and (sqlc.narg(zone_id)::uuid is null or b.zone_id = sqlc.narg(zone_id)::uuid)
  and (b.on_hand <> 0 or b.in_transit <> 0 or b.deficit <> 0)
  and (not @searching::boolean
       or v.article ilike '%' || sqlc.narg(article)::text || '%'
       or p.search @@ to_tsquery('russian', sqlc.narg(words)::text)
       or v.alcocode = sqlc.narg(alcocode)::text
       or v.gtin = sqlc.narg(gtin)::text
       or v.id = sqlc.narg(variant_id)::uuid
       or v.id in (select bc.variant_id from barcodes bc where bc.code = sqlc.narg(code)::text))
  and (sqlc.narg(after)::uuid is null
       or (p.name, coalesce(v.volume_ml, 0), v.id) >
          (select xp.name, coalesce(xv.volume_ml, 0), xv.id
           from variants xv join products xp on xp.id = xv.product_id
           where xv.id = sqlc.narg(after)::uuid))
order by p.name, coalesce(v.volume_ml, 0), v.id, z.kind
limit @max_rows;

-- name: ListMoves :many
-- Карточка движения: новые сверху, у каждого движения — фактический остаток зоны после него.
select t.id, t.posted_at, t.doc_date, t.document_id, t.document_number, t.document_kind,
       t.bucket, t.qty, t.cost_kopecks, t.on_hand_after
from (select m.id, m.posted_at, m.doc_date, m.document_id, d.number as document_number, d.kind as document_kind,
             m.bucket, m.qty, (m.cost_amount * 100)::bigint as cost_kopecks,
             coalesce(sum(m.qty) filter (where m.bucket = 'on_hand') over (order by m.id), 0)::integer as on_hand_after
      from stock_moves m
      join documents d on d.id = m.document_id
      where m.variant_id = @variant_id and m.zone_id = @zone_id) as t
where sqlc.narg(before)::bigint is null or t.id < sqlc.narg(before)::bigint
order by t.id desc
limit @max_rows;

-- name: MoveCardHeader :one
-- Шапка карточки движения: что за вариант и какая зона.
select v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       l.name as location_name, z.kind as zone_kind
from variants v
join products p on p.id = v.product_id
cross join zones z
join locations l on l.id = z.location_id
where v.id = @variant_id and z.id = @zone_id;
