-- name: InsertEntry :exec
insert into audit_log (user_id, session_id, ip, action, entity_type, entity_id, data)
values (sqlc.narg(user_id), sqlc.narg(session_id), nullif(@ip::text, '')::inet, @action,
        sqlc.narg(entity_type), sqlc.narg(entity_id), @data);

-- name: ListEntries :many
-- Пустой фильтр (null) не ограничивает выборку.
select a.id, a.at, a.action, a.user_id, u.login as user_login, u.full_name as user_full_name,
       a.session_id, a.ip, a.entity_type, a.entity_id, a.data
from audit_log a
left join users u on u.id = a.user_id
where (sqlc.narg(before)::bigint is null or a.id < sqlc.narg(before))
  and (sqlc.narg(from_at)::timestamptz is null or a.at >= sqlc.narg(from_at))
  and (sqlc.narg(to_at)::timestamptz is null or a.at < sqlc.narg(to_at))
  and (sqlc.narg(user_id)::uuid is null or a.user_id = sqlc.narg(user_id))
  and (sqlc.narg(action)::text is null or a.action = sqlc.narg(action))
  and (sqlc.narg(entity_type)::text is null or a.entity_type = sqlc.narg(entity_type))
  and (sqlc.narg(entity_id)::text is null or a.entity_id = sqlc.narg(entity_id))
order by a.id desc
limit @max_rows;
