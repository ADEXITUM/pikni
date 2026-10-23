-- name: GetLocationCode :one
select code
from locations
where id = @id;

-- name: ListAssortment :many
select a.variant_id, v.product_id, p.article, p.name, v.volume_ml, v.container, v.gift_pack,
       (v.is_active and p.is_active)::boolean as is_active, a.min_qty
from assortment_matrix a
join variants v on v.id = a.variant_id
join products p on p.id = v.product_id
where a.location_id = @location_id
order by p.name, v.volume_ml nulls first, v.container nulls first, v.gift_pack;

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
