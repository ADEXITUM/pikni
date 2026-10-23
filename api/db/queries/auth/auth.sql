-- name: GetUserByLogin :one
select id, login, full_name, role, password_hash, must_change_password, is_blocked, locked_until
from users
where lower(login) = lower(@login);

-- name: RecordFailedLogin :exec
-- Счётчик обнуляется в момент блокировки: после неё снова доступно max_attempts попыток.
update users
set failed_logins = case when failed_logins + 1 >= @max_attempts::int then 0 else failed_logins + 1 end,
    locked_until  = case when failed_logins + 1 >= @max_attempts::int then @lock_until::timestamptz else locked_until end
where id = @id;

-- name: ResetFailedLogins :exec
update users
set failed_logins = 0,
    locked_until  = null
where id = @id
  and (failed_logins > 0 or locked_until is not null);

-- name: CreateSession :one
insert into sessions (user_id, token_hash, csrf_token, ip, user_agent, last_seen_at, expires_at)
values (@user_id, @token_hash, @csrf_token, nullif(@ip::text, '')::inet, nullif(@user_agent::text, ''),
        @last_seen_at, @expires_at)
returning id;

-- name: GetSession :one
select s.id, s.user_id, s.csrf_token, s.last_seen_at, s.expires_at,
       u.login, u.full_name, u.role, u.must_change_password, u.is_blocked
from sessions s
join users u on u.id = s.user_id
where s.token_hash = @token_hash;

-- name: TouchSession :exec
update sessions
set last_seen_at = @last_seen_at
where id = @id;

-- name: DeleteSession :exec
delete from sessions
where id = @id;

-- name: DeleteOtherSessions :exec
delete from sessions
where user_id = @user_id
  and id <> @keep_id;

-- name: GetPasswordHash :one
select password_hash
from users
where id = @id;

-- name: SetPassword :exec
update users
set password_hash        = @password_hash,
    must_change_password = false,
    version              = version + 1,
    updated_at           = now()
where id = @id;
