-- name: CreateUser :one
insert into users (login, full_name, role, password_hash)
values ($1, $2, $3, $4)
returning id;

-- name: ResetPassword :one
update users
set password_hash        = sqlc.arg(password_hash),
    must_change_password = true,
    failed_logins        = 0,
    locked_until         = null,
    version              = version + 1,
    updated_at           = now()
where lower(login) = lower(sqlc.arg(login))
returning id;

-- name: DeleteUserSessions :exec
delete from sessions
where user_id = $1;
