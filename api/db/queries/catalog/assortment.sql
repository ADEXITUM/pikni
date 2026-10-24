-- name: GetLocationCode :one
select code
from locations
where id = @id;

-- name: ListAssortment :many
-- В магазине тысячи вариантов, поэтому матрица отдаётся страницами по (наименование, объём, id)
-- после варианта after. Поиск — как в каталоге: часть артикула, слова наименования или код варианта.
select a.variant_id, v.product_id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       (v.is_active and p.is_active)::boolean as is_active, a.min_qty
from assortment_matrix a
join variants v on v.id = a.variant_id
join products p on p.id = v.product_id
where a.location_id = @location_id
  and (not @searching::boolean
       or v.article ilike '%' || sqlc.narg(article)::text || '%'
       or p.search @@ to_tsquery('russian', sqlc.narg(words)::text)
       or v.alcocode = sqlc.narg(alcocode)::text
       or v.gtin = sqlc.narg(gtin)::text
       or v.id in (select b.variant_id from barcodes b where b.code = sqlc.narg(code)::text))
  and (sqlc.narg(after)::uuid is null
       or (p.name, coalesce(v.volume_ml, 0), v.id) >
          (select xp.name, coalesce(xv.volume_ml, 0), xv.id
           from variants xv join products xp on xp.id = xv.product_id
           where xv.id = sqlc.narg(after)::uuid))
order by p.name, coalesce(v.volume_ml, 0), v.id
limit @max_rows;

-- name: CountAssortment :one
select count(*)
from assortment_matrix a
join variants v on v.id = a.variant_id
join products p on p.id = v.product_id
where a.location_id = @location_id
  and (not @searching::boolean
       or v.article ilike '%' || sqlc.narg(article)::text || '%'
       or p.search @@ to_tsquery('russian', sqlc.narg(words)::text)
       or v.alcocode = sqlc.narg(alcocode)::text
       or v.gtin = sqlc.narg(gtin)::text
       or v.id in (select b.variant_id from barcodes b where b.code = sqlc.narg(code)::text));

-- name: GetAssortmentItem :one
select min_qty
from assortment_matrix
where location_id = @location_id and variant_id = @variant_id
for update;

-- name: UpsertAssortmentItem :exec
insert into assortment_matrix (location_id, variant_id, min_qty)
values (@location_id, @variant_id, @min_qty)
on conflict (location_id, variant_id) do update set min_qty = excluded.min_qty;

-- name: DeleteAssortmentItem :one
delete from assortment_matrix
where location_id = @location_id and variant_id = @variant_id
returning min_qty;

-- name: CopyAssortment :execrows
-- Уже имеющиеся в точке варианты не трогаются: копирование только добавляет.
insert into assortment_matrix (location_id, variant_id, min_qty)
select @to_location_id, a.variant_id, a.min_qty
from assortment_matrix a
where a.location_id = @from_location_id
on conflict (location_id, variant_id) do nothing;
