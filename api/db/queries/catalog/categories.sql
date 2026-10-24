-- name: ListCategories :many
select id, parent_id, name, depth, sort, is_active, alcohol
from categories
order by depth, sort, name;

-- name: GetCategory :one
select id, parent_id, name, depth, sort, is_active, alcohol
from categories
where id = @id
for update;

-- name: CreateCategory :one
-- У подкатегории признак алкоголя всё равно берёт триггер от родителя.
insert into categories (parent_id, name, sort, alcohol)
values (sqlc.narg(parent_id), @name, @sort, @alcohol)
returning id, parent_id, name, depth, sort, is_active, alcohol;

-- name: UpdateCategory :one
update categories
set name = @name, sort = @sort, is_active = @is_active
where id = @id
returning id, parent_id, name, depth, sort, is_active, alcohol;

-- name: SetCategoryTreeAlcohol :exec
-- Признак алкоголя меняется у категории верхнего уровня и сразу у всех её подкатегорий.
with recursive tree as (
    select c.id from categories c where c.id = @id::uuid
    union all
    select c.id from categories c join tree t on c.parent_id = t.id
)
update categories set alcohol = @alcohol where id in (select id from tree);

-- name: CategoryTreeHasAlcoholProducts :one
with recursive tree as (
    select c.id from categories c where c.id = @id::uuid
    union all
    select c.id from categories c join tree t on c.parent_id = t.id
)
select exists (select 1 from products p
               where p.category_id in (select id from tree) and (p.kind_code is not null or p.abv is not null));

-- name: IsAlcoholCategory :one
select alcohol from categories where id = @id;

-- name: HasActiveSubcategories :one
select exists (select 1 from categories where parent_id = @id::uuid and is_active);

-- name: ListCategoryAttributes :many
select id, category_id, code, name, type, unit, options, filterable, used_for_analogs, sort, is_active
from category_attributes
order by sort, name;

-- name: GetCategoryAttribute :one
select id, category_id, code, name, type, unit, options, filterable, used_for_analogs, sort, is_active
from category_attributes
where id = @id
for update;

-- name: CreateCategoryAttribute :one
insert into category_attributes (category_id, code, name, type, unit, options, filterable, used_for_analogs, sort)
values (@category_id, @code, @name, @type, sqlc.narg(unit), @options, @filterable, @used_for_analogs, @sort)
returning id, category_id, code, name, type, unit, options, filterable, used_for_analogs, sort, is_active;

-- name: UpdateCategoryAttribute :one
update category_attributes
set name = @name, unit = sqlc.narg(unit), options = @options, filterable = @filterable,
    used_for_analogs = @used_for_analogs, sort = @sort, is_active = @is_active
where id = @id
returning id, category_id, code, name, type, unit, options, filterable, used_for_analogs, sort, is_active;

-- Код занят, если он есть у самой категории, у её предков или потомков: иначе позиция
-- получила бы две характеристики с одним кодом.
-- name: AttributeCodeTaken :one
with recursive
    ancestors as (
        select c.id, c.parent_id from categories c where c.id = @category_id::uuid
        union all
        select c.id, c.parent_id from categories c join ancestors a on c.id = a.parent_id
    ),
    descendants as (
        select c.id from categories c where c.parent_id = @category_id::uuid
        union all
        select c.id from categories c join descendants d on c.parent_id = d.id
    )
select exists (
    select 1 from category_attributes ca
    where ca.code = @code
      and (ca.category_id in (select id from ancestors) or ca.category_id in (select id from descendants))
);
