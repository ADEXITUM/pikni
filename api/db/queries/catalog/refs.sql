-- name: ListCountries :many
select code, alpha2, name
from countries
order by name;

-- name: ListOriginRegions :many
select id, country_code, name, is_active
from origin_regions
order by name;

-- name: GetOriginRegion :one
select id, country_code, name, is_active
from origin_regions
where id = @id
for update;

-- name: CreateOriginRegion :one
insert into origin_regions (country_code, name)
values (@country_code, @name)
returning id, country_code, name, is_active;

-- name: UpdateOriginRegion :one
update origin_regions
set country_code = @country_code, name = @name, is_active = @is_active
where id = @id
returning id, country_code, name, is_active;

-- name: ListProducers :many
select id, name, inn, country_code, is_active
from producers
order by name;

-- name: GetProducer :one
select id, name, inn, country_code, is_active
from producers
where id = @id
for update;

-- name: CreateProducer :one
insert into producers (name, inn, country_code)
values (@name, sqlc.narg(inn), @country_code)
returning id, name, inn, country_code, is_active;

-- name: UpdateProducer :one
update producers
set name = @name, inn = sqlc.narg(inn), country_code = @country_code, is_active = @is_active
where id = @id
returning id, name, inn, country_code, is_active;

-- name: ListBrands :many
select id, producer_id, name, is_active
from brands
order by name;

-- name: GetBrand :one
select id, producer_id, name, is_active
from brands
where id = @id
for update;

-- name: CreateBrand :one
insert into brands (producer_id, name)
values (@producer_id, @name)
returning id, producer_id, name, is_active;

-- name: UpdateBrand :one
update brands
set producer_id = @producer_id, name = @name, is_active = @is_active
where id = @id
returning id, producer_id, name, is_active;

-- name: ListProductKinds :many
select k.code, k.name, k.egais_accounting, g.name as min_price_group
from product_kinds k
left join min_price_groups g on g.code = k.min_price_group
order by k.code;

-- name: ListVolumes :many
select ml, is_active
from volumes
order by ml;

-- name: GetVolume :one
select ml, is_active
from volumes
where ml = @ml
for update;

-- name: CreateVolume :one
insert into volumes (ml)
values (@ml)
returning ml, is_active;

-- name: SetVolumeActive :one
update volumes
set is_active = @is_active
where ml = @ml
returning ml, is_active;

-- name: ListWriteoffReasons :many
select id, name, egais_type, is_active
from writeoff_reasons
order by name;

-- name: GetWriteoffReason :one
select id, name, egais_type, is_active
from writeoff_reasons
where id = @id
for update;

-- name: CreateWriteoffReason :one
insert into writeoff_reasons (name, egais_type)
values (@name, @egais_type)
returning id, name, egais_type, is_active;

-- name: UpdateWriteoffReason :one
update writeoff_reasons
set name = @name, egais_type = @egais_type, is_active = @is_active
where id = @id
returning id, name, egais_type, is_active;
