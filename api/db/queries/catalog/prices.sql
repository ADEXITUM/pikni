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
select id, kind_code, valid_from, (price * 100)::bigint as kopecks, volume_ml, abv, abv_over, source
from min_retail_prices
order by kind_code, valid_from desc;

-- name: KindMinPriceRules :many
select id, kind_code, valid_from, (price * 100)::bigint as kopecks, volume_ml, abv, abv_over, source
from min_retail_prices
where kind_code = @kind_code
order by valid_from desc;

-- name: CreateMinPriceRule :one
insert into min_retail_prices (kind_code, valid_from, price, volume_ml, abv, abv_over, source)
values (@kind_code, @valid_from, sqlc.arg(kopecks)::bigint / 100.0, @volume_ml, sqlc.narg(abv), sqlc.narg(abv_over), @source)
returning id, kind_code, valid_from, (price * 100)::bigint as kopecks, volume_ml, abv, abv_over, source;

-- name: GetMinPriceRule :one
select id, kind_code, valid_from, (price * 100)::bigint as kopecks, volume_ml, abv, abv_over, source
from min_retail_prices
where id = @id
for update;

-- name: DeleteMinPriceRule :exec
delete from min_retail_prices
where id = @id;
