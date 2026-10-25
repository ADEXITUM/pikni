-- name: GetUserByLogin :one
select u.id, u.login, u.full_name, u.role, u.password_hash, u.must_change_password, u.is_blocked, u.locked_until,
       u.totp_enabled, array(select location_id from user_locations where user_id = u.id)::uuid[] as location_ids
from users u
where lower(u.login) = lower(@login);

-- name: GetUserByID :one
select u.id, u.login, u.full_name, u.role, u.password_hash, u.must_change_password, u.is_blocked, u.locked_until,
       u.totp_enabled, array(select location_id from user_locations where user_id = u.id)::uuid[] as location_ids
from users u
where u.id = @id;

-- name: RecordFailedLogin :one
-- Счётчик обнуляется в момент блокировки: после неё снова доступно max_attempts попыток.
-- Поэтому ноль после обновления означает, что именно эта попытка заблокировала вход.
update users
set failed_logins = case when failed_logins + 1 >= @max_attempts::int then 0 else failed_logins + 1 end,
    locked_until  = case when failed_logins + 1 >= @max_attempts::int then @lock_until::timestamptz else locked_until end
where id = @id
returning failed_logins = 0 as locked;

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
       u.login, u.full_name, u.role, u.must_change_password, u.is_blocked, u.totp_enabled,
       array(select location_id from user_locations where user_id = u.id)::uuid[] as location_ids
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

-- name: DeleteOtherSessions :execrows
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

-- name: ListLocationIDs :many
select id
from locations
order by code;

-- name: ListUserSessions :many
select id, ip, user_agent, created_at, last_seen_at
from sessions
where user_id = @user_id
  and expires_at > @now
  and last_seen_at > @idle_since
order by last_seen_at desc;

-- name: DeleteUserSession :execrows
delete from sessions
where id = @id
  and user_id = @user_id;

-- name: CreateLoginChallenge :exec
insert into login_challenges (token_hash, user_id, expires_at) values (@token_hash, @user_id, @expires_at);

-- name: TakeLoginChallenge :one
-- Попытки считаются: после пяти неверных кодов вызов сгорает.
update login_challenges set attempts = attempts + 1
where token_hash = @token_hash and expires_at > now() and attempts < 5
returning user_id, attempts;

-- name: DeleteLoginChallenge :exec
delete from login_challenges where token_hash = @token_hash or expires_at < now();

-- name: GetTOTP :one
select totp_secret, totp_pending, totp_enabled, role from users where id = @id;

-- name: SetTOTPPending :exec
update users set totp_pending = @secret where id = @id;

-- name: EnableTOTP :exec
update users set totp_secret = totp_pending, totp_pending = null, totp_enabled = true where id = @id;

-- name: DisableTOTP :exec
update users set totp_secret = null, totp_pending = null, totp_enabled = false where id = @id;

-- name: DeleteRecoveryCodes :exec
delete from totp_recovery_codes where user_id = @user_id;

-- name: InsertRecoveryCode :exec
insert into totp_recovery_codes (user_id, code_hash) values (@user_id, @code_hash);

-- name: UseRecoveryCode :execrows
update totp_recovery_codes set used_at = now() where user_id = @user_id and code_hash = @code_hash and used_at is null;

-- name: CreateQRLogin :exec
insert into qr_logins (code_hash, user_id, expires_at) values (@code_hash, @user_id, @expires_at);

-- name: TakeQRLogin :one
update qr_logins set used_at = now()
where code_hash = @code_hash and used_at is null and expires_at > now()
returning user_id;
