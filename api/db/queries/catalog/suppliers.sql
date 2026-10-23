-- name: ListSuppliers :many
select id, name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts, is_active
from suppliers
order by name;

-- name: GetSupplier :one
select id, name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts, is_active
from suppliers
where id = @id
for update;

-- name: CreateSupplier :one
insert into suppliers (name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts)
values (@name, @inn, sqlc.narg(kpp), sqlc.narg(fsrar_id), sqlc.narg(license_no), sqlc.narg(license_valid_to),
        @default_receiving_mode, @contacts)
returning id, name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts, is_active;

-- name: UpdateSupplier :one
update suppliers
set name = @name, inn = @inn, kpp = sqlc.narg(kpp), fsrar_id = sqlc.narg(fsrar_id), license_no = sqlc.narg(license_no),
    license_valid_to = sqlc.narg(license_valid_to), default_receiving_mode = @default_receiving_mode,
    contacts = @contacts, is_active = @is_active
where id = @id
returning id, name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts, is_active;
