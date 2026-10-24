-- Наличие по сети (НЛ-1 – НЛ-3, НЛ-6). Остатки — «на руках» (on_hand) без резерва и недостачи.

-- name: SearchAvailability :many
-- Пустой фильтр (null) не ограничивает выборку. Точки фильтров (город, точка, тип) задают, где
-- считать остаток и что значит «только в наличии»; «здесь» — текущая точка пользователя.
-- Характеристики категории: attrs — точное совпадение (@>), ranges — {"код": {"min": 3, "max": 10}}.
with recursive subtree as (
    select c.id from categories c where c.id = sqlc.narg(category_id)::uuid
    union all
    select c.id from categories c join subtree s on c.parent_id = s.id
),
scope as (
    select l.id from locations l
    where l.is_active
      and (sqlc.narg(city)::text is null or l.city = sqlc.narg(city)::text)
      and (sqlc.narg(location_id)::uuid is null or l.id = sqlc.narg(location_id)::uuid)
      and (sqlc.narg(location_kind)::text is null or l.kind = sqlc.narg(location_kind)::text)
),
retail as (
    select distinct on (pr.variant_id) pr.variant_id, (pr.price * 100)::bigint as kopecks
    from prices pr
    where pr.kind = 'retail' and pr.valid_from <= now()
    order by pr.variant_id, pr.valid_from desc
),
stock as (
    select b.variant_id,
           coalesce(sum(b.on_hand) filter (where b.location_id in (select id from scope)), 0)::integer as scope_qty,
           sum(b.on_hand)::integer as network_qty,
           coalesce(sum(b.on_hand) filter (where b.location_id = sqlc.narg(here)::uuid), 0)::integer as here_qty
    from stock_balances b
    group by b.variant_id
),
vs as (
    select v.id, v.product_id, r.kopecks,
           coalesce(s.scope_qty, 0) as scope_qty, coalesce(s.network_qty, 0) as network_qty,
           coalesce(s.here_qty, 0) as here_qty
    from variants v
    left join retail r on r.variant_id = v.id
    left join stock s on s.variant_id = v.id
    where v.is_active
      and (sqlc.narg(volume_ml)::integer is null or v.volume_ml = sqlc.narg(volume_ml)::integer)
      and (sqlc.narg(price_from)::bigint is null or r.kopecks >= sqlc.narg(price_from)::bigint)
      and (sqlc.narg(price_to)::bigint is null or r.kopecks <= sqlc.narg(price_to)::bigint)
)
select c.id, c.name, c.category_id, c.category_name, c.kind_code, c.egais_accounting, coalesce(c.abv, 0)::float8 as abv, c.brand_name,
       c.country_code, c.country_name, c.line_name, c.variety,
       count(vs.id)::integer as variants,
       coalesce(min(vs.kopecks), 0)::bigint as min_price,
       coalesce(max(vs.kopecks), 0)::bigint as max_price,
       sum(vs.scope_qty)::integer as scope_qty,
       sum(vs.network_qty)::integer as network_qty,
       sum(vs.here_qty)::integer as here_qty,
       (count(*) over ())::integer as total
from product_cards c
join products p on p.id = c.id
join vs on vs.product_id = c.id
where c.is_active
  and (not @searching::boolean
       or c.id = sqlc.narg(product_id)::uuid
       or c.id in (select v.product_id from variants v where v.article ilike '%' || sqlc.narg(article)::text || '%')
       or c.id in (select x.id from products x where x.search @@ to_tsquery('russian', sqlc.narg(words)::text))
       or c.id in (select v.product_id from variants v
                   where v.alcocode = sqlc.narg(alcocode)::text or v.gtin = sqlc.narg(gtin)::text)
       or c.id in (select v.product_id from barcodes b join variants v on v.id = b.variant_id
                   where b.code = sqlc.narg(code)::text))
  and (sqlc.narg(category_id)::uuid is null or c.category_id in (select s.id from subtree s))
  and (sqlc.narg(kind_code)::text is null or c.kind_code = sqlc.narg(kind_code)::text)
  and (sqlc.narg(brand_id)::uuid is null or c.brand_id = sqlc.narg(brand_id)::uuid)
  and (sqlc.narg(country_code)::text is null or c.country_code = sqlc.narg(country_code)::text)
  and (sqlc.narg(abv_from)::float8 is null or c.abv >= sqlc.narg(abv_from)::float8)
  and (sqlc.narg(abv_to)::float8 is null or c.abv <= sqlc.narg(abv_to)::float8)
  and (sqlc.narg(attrs)::jsonb is null or c.attributes @> sqlc.narg(attrs)::jsonb)
  and (sqlc.narg(ranges)::jsonb is null or not exists (
        select 1 from jsonb_each(sqlc.narg(ranges)::jsonb) r
        where jsonb_typeof(c.attributes -> r.key) is distinct from 'number'
           or (c.attributes ->> r.key)::numeric < coalesce((r.value ->> 'min')::numeric, '-Infinity')
           or (c.attributes ->> r.key)::numeric > coalesce((r.value ->> 'max')::numeric, 'Infinity')))
group by c.id, c.name, c.category_id, c.category_name, c.kind_code, c.egais_accounting, c.abv, c.brand_name,
         c.country_code, c.country_name, c.line_name, c.variety, p.created_at
having not @in_stock::boolean or sum(vs.scope_qty) > 0
order by
    case when @sort::text = 'price' then min(vs.kopecks) end asc nulls last,
    case when @sort::text = 'stock' then sum(vs.scope_qty) end desc,
    case when @sort::text = 'new' then p.created_at end desc,
    c.name, c.id
offset @row_offset
limit @max_rows;

-- name: VariantProduct :one
select product_id from variants where id = @id;

-- name: AvailabilityProduct :one
select c.id, c.name, c.category_id, c.category_name, c.kind_code, c.egais_accounting,
       coalesce(c.abv, 0)::float8 as abv, c.brand_name, c.country_name, c.line_name, c.product_line_id,
       c.variety, c.attributes, c.crpt_marked
from product_cards c
where c.id = @id and c.is_active;

-- name: AvailabilitySchema :many
-- Характеристики категории позиции вместе с унаследованными: для подписей и подбора аналогов.
with recursive line as (
    select c.id, c.parent_id from categories c where c.id = @category_id::uuid
    union all
    select c.id, c.parent_id from categories c join line l on c.id = l.parent_id
)
select a.code, a.name, a.type, a.unit, a.options, a.used_for_analogs
from category_attributes a
where a.category_id in (select l.id from line l) and a.is_active
order by a.sort, a.name;

-- name: AvailabilityLocations :many
select id, code, name, city, kind, lat, lon
from locations
where is_active
order by city, kind desc, code;

-- name: AvailabilityVariants :many
select v.id, v.article, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       coalesce((select (pr.price * 100)::bigint from prices pr
                 where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
                 order by pr.valid_from desc limit 1), 0)::bigint as price_kopecks
from variants v
where v.product_id = @product_id and v.is_active
order by v.volume_ml nulls last, v.net_weight_g nulls last, v.container, v.gift_pack;

-- name: AvailabilityCells :many
-- Ячейки «вариант × точка»: в зале, на складе (склад магазина или распределительный), резерв, в пути.
select b.variant_id, b.location_id,
       coalesce(sum(b.on_hand) filter (where z.kind = 'floor'), 0)::integer as floor,
       coalesce(sum(b.on_hand) filter (where z.kind <> 'floor'), 0)::integer as storage,
       sum(b.reserved)::integer as reserved,
       sum(b.in_transit)::integer as in_transit
from stock_balances b
join zones z on z.id = b.zone_id
join variants v on v.id = b.variant_id
where v.product_id = @product_id
group by b.variant_id, b.location_id
having sum(b.on_hand) + sum(b.in_transit) > 0;

-- name: SimilarProducts :many
-- Похожие товары (НЛ-6) для варианта: pack — другой объём или тара той же позиции; line — другие
-- разновидности той же линейки; analog — тот же вид продукции и объём, цена в пределах ±15 %,
-- совпадают характеристики «для аналогов» и товар есть «здесь» (без текущей точки — где-нибудь в сети).
with recursive line as (
    select c.id, c.parent_id from categories c
    where c.id = (select p.category_id from variants v join products p on p.id = v.product_id where v.id = @variant_id)
    union all
    select c.id, c.parent_id from categories c join line l on c.id = l.parent_id
),
base as (
    select v.id, v.product_id, v.volume_ml, p.kind_code, p.product_line_id, p.attributes,
           (select (pr.price * 100)::bigint from prices pr
            where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
            order by pr.valid_from desc limit 1) as kopecks
    from variants v join products p on p.id = v.product_id
    where v.id = @variant_id
),
keys as (
    select coalesce(jsonb_object_agg(a.code, b.attributes -> a.code)
                    filter (where b.attributes ? a.code), '{}') as analog_attrs
    from base b
    join category_attributes a on a.used_for_analogs and a.is_active and a.category_id in (select id from line)
),
candidates as (
    select v.id as variant_id, p.id as product_id, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
           coalesce((select (pr.price * 100)::bigint from prices pr
                     where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
                     order by pr.valid_from desc limit 1), 0)::bigint as price_kopecks,
           coalesce((select sum(s.on_hand) from stock_balances s
                     where s.variant_id = v.id and s.location_id = sqlc.narg(here)::uuid), 0)::integer as here_qty,
           coalesce((select sum(s.on_hand) from stock_balances s where s.variant_id = v.id), 0)::integer as network_qty,
           case
               when p.id = (select product_id from base) then 'pack'
               when p.product_line_id = (select product_line_id from base) then 'line'
               else 'analog'
           end::text as grp,
           p.kind_code, p.attributes
    from variants v
    join products p on p.id = v.product_id
    where v.is_active and p.is_active and v.id <> @variant_id
      and (p.id = (select product_id from base)
           or p.product_line_id = (select product_line_id from base)
           or (p.kind_code = (select kind_code from base) and v.volume_ml is not distinct from (select volume_ml from base)))
)
select c.variant_id, c.product_id, c.name, c.volume_ml, c.net_weight_g, c.container, c.gift_pack,
       c.price_kopecks, c.here_qty, c.network_qty, c.grp
from candidates c, base b, keys k
where c.grp <> 'analog'
   or (b.kopecks is not null and c.price_kopecks between b.kopecks * 0.85 and b.kopecks * 1.15
       and c.attributes @> k.analog_attrs
       and (case when sqlc.narg(here)::uuid is null then c.network_qty else c.here_qty end) > 0)
order by c.grp, abs(c.price_kopecks - coalesce(b.kopecks, 0)), c.name
limit 60;
