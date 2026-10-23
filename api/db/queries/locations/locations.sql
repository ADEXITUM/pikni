-- name: ListLocations :many
select id, code, name, kind, city, timezone, subject_code, address, lat, lon, fsrar_id, kpp,
       license_no, license_valid_to, opening_hours, is_public, is_active, version
from locations
order by code;

-- name: GetLocation :one
select id, code, name, kind, city, timezone, subject_code, address, lat, lon, fsrar_id, kpp,
       license_no, license_valid_to, opening_hours, is_public, is_active, version
from locations
where id = @id;

-- name: LockLocation :one
select id, code, name, kind, city, timezone, subject_code, address, lat, lon, fsrar_id, kpp,
       license_no, license_valid_to, opening_hours, is_public, is_active, version
from locations
where id = @id
for update;

-- name: CreateLocation :one
insert into locations (code, name, kind, city, timezone, subject_code, address, lat, lon, fsrar_id, kpp,
                       license_no, license_valid_to, opening_hours, is_public)
values (@code, @name, @kind, @city, @timezone, @subject_code, sqlc.narg(address), sqlc.narg(lat), sqlc.narg(lon),
        sqlc.narg(fsrar_id), sqlc.narg(kpp), sqlc.narg(license_no), sqlc.narg(license_valid_to),
        @opening_hours, @is_public)
returning id;

-- name: UpdateLocation :exec
update locations
set name             = @name,
    city             = @city,
    timezone         = @timezone,
    subject_code     = @subject_code,
    address          = sqlc.narg(address),
    lat              = sqlc.narg(lat),
    lon              = sqlc.narg(lon),
    fsrar_id         = sqlc.narg(fsrar_id),
    kpp              = sqlc.narg(kpp),
    license_no       = sqlc.narg(license_no),
    license_valid_to = sqlc.narg(license_valid_to),
    opening_hours    = @opening_hours,
    is_public        = @is_public,
    is_active        = @is_active,
    version          = version + 1,
    updated_at       = now()
where id = @id;

-- name: ListZones :many
select id, location_id, kind, name
from zones
order by location_id, kind;

-- name: ListLocationZones :many
select id, location_id, kind, name
from zones
where location_id = @location_id
order by kind;

-- name: ListRuSubjects :many
select code, name
from ru_subjects
order by name;
