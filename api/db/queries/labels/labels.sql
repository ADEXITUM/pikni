-- name: PriceTagItems :many
-- Данные ценников: текущая розничная цена, основной штрихкод (EAN-13 производителя, иначе
-- внутренний) и цена последнего напечатанного ценника в точке.
select v.id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack, p.abv,
       coalesce((select (pr.price * 100)::bigint from prices pr
                 where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
                 order by pr.valid_from desc limit 1), 0)::bigint as price_kopecks,
       coalesce((select b.code from barcodes b where b.variant_id = v.id and b.kind in ('ean13', 'internal')
                 order by (b.kind = 'internal'), b.code limit 1), '')::text as barcode,
       coalesce((t.price * 100)::bigint, 0)::bigint as printed_kopecks, t.printed_at
from variants v
join products p on p.id = v.product_id
left join price_tag_prints t on t.variant_id = v.id and t.location_id = @location_id
where v.id = any(@variant_ids::uuid[])
order by p.name, v.volume_ml;

-- name: StaleTags :many
-- Ценники к перепечатке (ЭТ-3): товар есть в торговом зале точки, а цена изменилась после
-- печати ценника или ценник ещё не печатался.
select v.id
from variants v
join products p on p.id = v.product_id
where v.is_active
  and exists (select 1 from stock_balances b join zones z on z.id = b.zone_id
              where b.variant_id = v.id and b.location_id = @location_id and z.kind = 'floor' and b.on_hand > 0)
  and coalesce((select pr.price from prices pr
                where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
                order by pr.valid_from desc limit 1), 0)
      is distinct from (select t.price from price_tag_prints t where t.variant_id = v.id and t.location_id = @location_id)
order by p.name;

-- name: DocumentVariants :many
select distinct variant_id from document_lines where document_id = $1;

-- name: DocumentLocation :one
select location_id, kind from documents where id = $1;

-- name: RecordPrint :exec
insert into price_tag_prints (location_id, variant_id, price, printed_by)
select @location_id, v, coalesce((select pr.price from prices pr
                                  where pr.variant_id = v and pr.kind = 'retail' and pr.valid_from <= now()
                                  order by pr.valid_from desc limit 1), 0), sqlc.narg(printed_by)
from unnest(@variant_ids::uuid[]) as v
on conflict (location_id, variant_id) do update set price = excluded.price, printed_at = now(),
    printed_by = excluded.printed_by;
