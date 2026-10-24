-- name: ListProductLines :many
select id, brand_id, name, is_active
from product_lines
order by name;

-- name: GetProductLine :one
select id, brand_id, name, is_active
from product_lines
where id = @id
for update;

-- name: CreateProductLine :one
insert into product_lines (brand_id, name)
values (@brand_id, @name)
returning id, brand_id, name, is_active;

-- name: UpdateProductLine :one
update product_lines
set name = @name, is_active = @is_active
where id = @id
returning id, brand_id, name, is_active;

-- name: ListProducts :many
-- Пустой фильтр (null) не ограничивает выборку. Страницы идут по (name, id) после позиции after.
-- При поиске позиция подходит, если совпало хоть что-то: часть артикула, слова наименования,
-- алкокод, GTIN или штрихкод одного из вариантов.
with recursive subtree as (
    select c.id from categories c where c.id = sqlc.narg(category_id)::uuid
    union all
    select c.id from categories c join subtree s on c.parent_id = s.id
)
select p.*
from product_cards p
where (sqlc.narg(after)::uuid is null
       or (p.name, p.id) > (select a.name, a.id from products a where a.id = sqlc.narg(after)::uuid))
  and (not @searching::boolean
       or p.article ilike '%' || sqlc.narg(article)::text || '%'
       or p.id in (select x.id from products x where x.search @@ to_tsquery('russian', sqlc.narg(words)::text))
       or p.id in (select v.product_id from variants v
                   where v.alcocode = sqlc.narg(alcocode)::text or v.gtin = sqlc.narg(gtin)::text)
       or p.id in (select v.product_id from barcodes b join variants v on v.id = b.variant_id
                   where b.code = sqlc.narg(code)::text))
  and (sqlc.narg(category_id)::uuid is null or p.category_id in (select s.id from subtree s))
  and (sqlc.narg(brand_id)::uuid is null or p.brand_id = sqlc.narg(brand_id)::uuid)
order by p.name, p.id
limit @max_rows;

-- name: FindProductByArticle :one
select id
from products
where article = @article;

-- name: GetProduct :one
select *
from product_cards
where id = @id;

-- name: LockProductVersion :one
select version
from products
where id = @id
for update;

-- name: CreateProduct :one
insert into products (name, category_id, kind_code, abv, crpt_marked, producer_id, brand_id,
                      product_line_id, variety, country_code, origin_region_id, attributes)
values (@name, @category_id, sqlc.narg(kind_code), sqlc.narg(abv), @crpt_marked, @producer_id,
        sqlc.narg(brand_id), sqlc.narg(product_line_id), sqlc.narg(variety), @country_code,
        sqlc.narg(origin_region_id), @attributes)
returning id;

-- name: UpdateProduct :exec
update products
set name             = @name,
    category_id      = @category_id,
    kind_code        = sqlc.narg(kind_code),
    abv              = sqlc.narg(abv),
    crpt_marked      = @crpt_marked,
    producer_id      = @producer_id,
    brand_id         = sqlc.narg(brand_id),
    product_line_id  = sqlc.narg(product_line_id),
    variety          = sqlc.narg(variety),
    country_code     = @country_code,
    origin_region_id = sqlc.narg(origin_region_id),
    attributes       = @attributes,
    is_active        = @is_active,
    version          = version + 1,
    updated_at       = now()
where id = @id;

-- name: CategoryIsActive :one
select is_active
from categories
where id = @id;

-- name: CategorySchema :many
-- Характеристики категории вместе с унаследованными от предков.
with recursive line as (
    select c.id, c.parent_id from categories c where c.id = @category_id::uuid
    union all
    select c.id, c.parent_id from categories c join line l on c.id = l.parent_id
)
select a.id, a.category_id, a.code, a.name, a.type, a.unit, a.options, a.filterable,
       a.used_for_analogs, a.sort, a.is_active
from category_attributes a
where a.category_id in (select l.id from line l);
