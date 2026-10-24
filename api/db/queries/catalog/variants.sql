-- name: GetVariantProduct :one
select id, name, kind_code, abv
from products
where id = @id;

-- name: ListProductVariants :many
select id, product_id, volume_ml, container, gift_pack, alcocode, gtin, is_active, created_at, article
from variants
where product_id = @product_id
order by volume_ml nulls first, container nulls first, gift_pack;

-- name: ListProductBarcodes :many
select b.code, b.variant_id, b.kind, b.created_at
from barcodes b
join variants v on v.id = b.variant_id
where v.product_id = @product_id
order by b.created_at, b.code;

-- name: GetVariant :one
select id, product_id, volume_ml, container, gift_pack, alcocode, gtin, is_active, created_at, article
from variants
where id = @id
for update;

-- name: CreateVariant :one
insert into variants (product_id, volume_ml, container, gift_pack, alcocode, gtin)
values (@product_id, sqlc.narg(volume_ml), sqlc.narg(container), @gift_pack, sqlc.narg(alcocode), sqlc.narg(gtin))
returning id, product_id, volume_ml, container, gift_pack, alcocode, gtin, is_active, created_at, article;

-- name: UpdateVariant :one
update variants
set alcocode = sqlc.narg(alcocode), gtin = sqlc.narg(gtin), is_active = @is_active
where id = @id
returning id, product_id, volume_ml, container, gift_pack, alcocode, gtin, is_active, created_at, article;

-- name: CreateBarcode :one
insert into barcodes (code, variant_id, kind)
values (@code, @variant_id, @kind)
returning code, variant_id, kind, created_at;

-- name: DeleteBarcode :one
delete from barcodes
where code = @code
returning code, variant_id, kind, created_at;

-- name: NextInternalBarcode :one
select nextval('internal_barcode_seq')::bigint;

-- name: FindVariantByCode :one
-- Вариант по штрихкоду, GTIN, алкокоду или артикулу — что из этого считал сканер или набрал человек.
select v.id, v.product_id
from variants v
left join barcodes b on b.variant_id = v.id and b.code = sqlc.narg(code)::text
where v.alcocode = sqlc.narg(alcocode)::text
   or v.gtin = sqlc.narg(gtin)::text
   or v.article = sqlc.narg(article)::text
   or b.code is not null
limit 1;
