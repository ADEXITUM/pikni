-- name: ListUsers :many
-- Поиск, фильтры и страница: сотрудников федеральной сети — тысячи (решение 0064).
select u.id, u.login, u.full_name, u.role, u.is_blocked, u.must_change_password, u.locked_until,
       u.version, u.created_at,
       array(select location_id from user_locations where user_id = u.id order by location_id)::uuid[] as location_ids
from users u
where (sqlc.narg(q)::text is null or u.login ilike '%' || sqlc.narg(q)::text || '%'
       or u.full_name ilike '%' || sqlc.narg(q)::text || '%')
  and (sqlc.narg(role)::text is null or u.role = sqlc.narg(role)::text)
  and (sqlc.narg(location_id)::uuid is null
       or exists (select 1 from user_locations ul where ul.user_id = u.id and ul.location_id = sqlc.narg(location_id)::uuid))
  and (sqlc.narg(status)::text is null
       or sqlc.narg(status)::text = 'blocked' and u.is_blocked
       or sqlc.narg(status)::text = 'active' and not u.is_blocked and not u.must_change_password
       or sqlc.narg(status)::text = 'temporary' and u.must_change_password and not u.is_blocked)
order by
    case when @sort::text = 'login' then u.login end,
    case when @sort::text = '-login' then u.login end desc,
    case when @sort::text = 'role' then u.role end,
    case when @sort::text = '-role' then u.role end desc,
    case when @sort::text = 'created' then u.created_at end,
    case when @sort::text = '-created' then u.created_at end desc,
    case when @sort::text = '-name' then u.full_name end desc,
    u.full_name, u.login
offset @row_offset
limit nullif(@max_rows::int, 0);

-- name: CountUsers :one
select count(*)::int from users u
where (sqlc.narg(q)::text is null or u.login ilike '%' || sqlc.narg(q)::text || '%'
       or u.full_name ilike '%' || sqlc.narg(q)::text || '%')
  and (sqlc.narg(role)::text is null or u.role = sqlc.narg(role)::text)
  and (sqlc.narg(location_id)::uuid is null
       or exists (select 1 from user_locations ul where ul.user_id = u.id and ul.location_id = sqlc.narg(location_id)::uuid))
  and (sqlc.narg(status)::text is null
       or sqlc.narg(status)::text = 'blocked' and u.is_blocked
       or sqlc.narg(status)::text = 'active' and not u.is_blocked and not u.must_change_password
       or sqlc.narg(status)::text = 'temporary' and u.must_change_password and not u.is_blocked);

-- name: GetUser :one
select u.id, u.login, u.full_name, u.role, u.is_blocked, u.must_change_password, u.locked_until,
       u.version, u.created_at,
       array(select location_id from user_locations where user_id = u.id order by location_id)::uuid[] as location_ids
from users u
where u.id = @id;

-- name: LockUser :one
-- Строка заблокирована до конца транзакции: версию не изменят между проверкой и записью.
select u.id, u.login, u.full_name, u.role, u.is_blocked, u.must_change_password, u.locked_until,
       u.version, u.created_at,
       array(select location_id from user_locations where user_id = u.id order by location_id)::uuid[] as location_ids
from users u
where u.id = @id
for update of u;

-- name: FindUserIDByLogin :one
select id
from users
where lower(login) = lower(@login);

-- name: CreateUser :one
insert into users (login, full_name, role, password_hash)
values (@login, @full_name, @role, @password_hash)
returning id;

-- name: UpdateUser :exec
update users
set full_name  = @full_name,
    role       = @role,
    version    = version + 1,
    updated_at = now()
where id = @id;

-- name: SetUserBlocked :exec
update users
set is_blocked = @is_blocked,
    version    = version + 1,
    updated_at = now()
where id = @id;

-- name: DeleteUserLocations :exec
delete from user_locations
where user_id = @user_id;

-- name: AddUserLocations :exec
insert into user_locations (user_id, location_id)
select @user_id::uuid, unnest(@location_ids::uuid[]);

-- name: ResetPassword :one
update users
set password_hash        = @password_hash,
    must_change_password = true,
    failed_logins        = 0,
    locked_until         = null,
    version              = version + 1,
    updated_at           = now()
where id = @id
returning login;

-- name: DeleteUserSessions :execrows
delete from sessions
where user_id = @user_id;
