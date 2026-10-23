-- Цены в базе — рубли numeric(12,2), в приложении — целые копейки.

-- name: ListVariantPrices :many
select p.id, p.variant_id, p.kind, (p.price * 100)::bigint as kopecks, p.valid_from, u.full_name as set_by
from prices p
left join users u on u.id = p.set_by
where p.variant_id = @variant_id and p.kind = @kind
order by p.valid_from desc;

-- name: CurrentProductPrices :many
select distinct on (p.variant_id, p.kind) p.variant_id, p.kind, (p.price * 100)::bigint as kopecks
from prices p
join variants v on v.id = p.variant_id
where v.product_id = @product_id and p.valid_from <= now()
order by p.variant_id, p.kind, p.valid_from desc;

-- name: CreatePrice :one
insert into prices (variant_id, kind, price, valid_from, set_by)
values (@variant_id, @kind, sqlc.arg(kopecks)::bigint / 100.0, @valid_from, sqlc.narg(set_by))
returning id, variant_id, kind, (price * 100)::bigint as kopecks, valid_from;

-- name: GetPrice :one
select id, variant_id, kind, (price * 100)::bigint as kopecks, valid_from
from prices
where id = @id
for update;

-- name: DeletePrice :exec
delete from prices
where id = @id;

-- name: ListMinPriceRules :many
select r.id, r.group_code, g.name as group_name, r.valid_from, r.abv_from, r.abv_to, (r.price * 100)::bigint as kopecks,
       r.volume_ml, r.same_price_over_ml, r.source
from min_retail_prices r
join min_price_groups g on g.code = r.group_code
order by g.name, r.valid_from desc, r.abv_from;

-- name: ListMinPriceGroups :many
select code, name
from min_price_groups
order by name;

-- name: KindMinPriceRules :many
-- Редакции МРЦ группы, к которой относится вид продукции; от новых к старым.
select r.id, r.group_code, g.name as group_name, r.valid_from, r.abv_from, r.abv_to, (r.price * 100)::bigint as kopecks,
       r.volume_ml, r.same_price_over_ml, r.source
from min_retail_prices r
join min_price_groups g on g.code = r.group_code
join product_kinds k on k.min_price_group = r.group_code
where k.code = @kind_code
order by r.valid_from desc, r.abv_from;

-- name: GetMinPriceRule :one
select r.id, r.group_code, g.name as group_name, r.valid_from, r.abv_from, r.abv_to, (r.price * 100)::bigint as kopecks,
       r.volume_ml, r.same_price_over_ml, r.source
from min_retail_prices r
join min_price_groups g on g.code = r.group_code
where r.id = @id
for update of r;

-- name: CreateMinPriceRule :one
insert into min_retail_prices (group_code, valid_from, abv_from, abv_to, price, volume_ml, same_price_over_ml, source)
values (@group_code, @valid_from, @abv_from, sqlc.narg(abv_to), sqlc.arg(kopecks)::bigint / 100.0, @volume_ml,
        sqlc.narg(same_price_over_ml), @source)
returning id;

-- name: DeleteMinPriceRule :exec
delete from min_retail_prices
where id = @id;
