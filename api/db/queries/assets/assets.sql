-- name: ListAssetCategories :many
select code, name from asset_categories order by name;

-- name: ListAssets :many
-- Ведомость имущества (ИМ-6): по точкам, МОЛ, категории и состоянию; списанные — по фильтру.
select a.id, a.number, a.name, a.category_code, c.name as category_name, a.serial, a.accepted_at,
       (a.cost * 100)::bigint as cost_kopecks, a.location_id, l.code as location_code, l.name as location_name,
       a.responsible_id, u.full_name as responsible_name, a.status, (a.photo is not null)::boolean as has_photo,
       a.version, a.updated_at
from assets a
join asset_categories c on c.code = a.category_code
join locations l on l.id = a.location_id
left join users u on u.id = a.responsible_id
where (sqlc.narg(location_ids)::uuid[] is null or a.location_id = any(sqlc.narg(location_ids)::uuid[]))
  and (sqlc.narg(location_id)::uuid is null or a.location_id = sqlc.narg(location_id)::uuid)
  and (sqlc.narg(responsible_id)::uuid is null or a.responsible_id = sqlc.narg(responsible_id)::uuid)
  and (sqlc.narg(category_code)::text is null or a.category_code = sqlc.narg(category_code)::text)
  and (case when sqlc.narg(status)::text is null then a.status <> 'written_off' else a.status = sqlc.narg(status)::text end)
  and (sqlc.narg(q)::text is null or a.number ilike '%' || sqlc.narg(q)::text || '%'
       or a.name ilike '%' || sqlc.narg(q)::text || '%' or a.serial ilike '%' || sqlc.narg(q)::text || '%')
order by l.code, a.number
offset @row_offset
limit nullif(@max_rows::int, 0);

-- name: CountAssets :one
-- Итог ведомости по тому же фильтру, что ListAssets: число объектов и стоимость.
select count(*)::integer as total, coalesce(sum(a.cost * 100), 0)::bigint as cost_kopecks
from assets a
where (sqlc.narg(location_ids)::uuid[] is null or a.location_id = any(sqlc.narg(location_ids)::uuid[]))
  and (sqlc.narg(location_id)::uuid is null or a.location_id = sqlc.narg(location_id)::uuid)
  and (sqlc.narg(responsible_id)::uuid is null or a.responsible_id = sqlc.narg(responsible_id)::uuid)
  and (sqlc.narg(category_code)::text is null or a.category_code = sqlc.narg(category_code)::text)
  and (case when sqlc.narg(status)::text is null then a.status <> 'written_off' else a.status = sqlc.narg(status)::text end)
  and (sqlc.narg(q)::text is null or a.number ilike '%' || sqlc.narg(q)::text || '%'
       or a.name ilike '%' || sqlc.narg(q)::text || '%' or a.serial ilike '%' || sqlc.narg(q)::text || '%');

-- name: GetAsset :one
select a.id, a.number, a.name, a.category_code, c.name as category_name, a.serial, a.accepted_at,
       (a.cost * 100)::bigint as cost_kopecks, a.location_id, l.code as location_code, l.name as location_name,
       a.responsible_id, u.full_name as responsible_name, a.status, (a.photo is not null)::boolean as has_photo,
       a.version, a.updated_at
from assets a
join asset_categories c on c.code = a.category_code
join locations l on l.id = a.location_id
left join users u on u.id = a.responsible_id
where a.id = $1;

-- name: AssetByNumber :one
select id, location_id from assets where number = upper(btrim(@number::text));

-- name: CreateAsset :one
insert into assets (number, name, category_code, serial, accepted_at, cost, location_id, responsible_id)
values ('INV-' || lpad(nextval('asset_number_seq')::text, 6, '0'), @name, @category_code, @serial, @accepted_at,
        (sqlc.arg(cost_kopecks)::bigint) / 100.0, @location_id, sqlc.narg(responsible_id))
returning id, number;

-- name: LockAsset :one
select id, number, name, location_id, responsible_id, status, version from assets where id = $1 for update;

-- name: UpdateAsset :exec
update assets set name = @name, category_code = @category_code, serial = @serial, accepted_at = @accepted_at,
       cost = (sqlc.arg(cost_kopecks)::bigint) / 100.0, version = version + 1, updated_at = now()
where id = @id;

-- name: SetAssetLocation :exec
update assets set location_id = @location_id, version = version + 1, updated_at = now() where id = @id;

-- name: SetAssetResponsible :exec
update assets set responsible_id = sqlc.narg(responsible_id), version = version + 1, updated_at = now() where id = @id;

-- name: SetAssetStatus :exec
update assets set status = @status, version = version + 1, updated_at = now() where id = @id;

-- name: SetAssetPhoto :exec
update assets set photo = sqlc.narg(photo), photo_type = sqlc.narg(photo_type), updated_at = now() where id = @id;

-- name: AssetPhoto :one
select photo, photo_type, location_id from assets where id = $1 and photo is not null;

-- name: InsertAssetEvent :exec
insert into asset_events (asset_id, kind, from_location_id, to_location_id, from_user_id, to_user_id,
                          from_status, to_status, comment, user_id)
values (@asset_id, @kind, sqlc.narg(from_location_id), sqlc.narg(to_location_id), sqlc.narg(from_user_id),
        sqlc.narg(to_user_id), sqlc.narg(from_status), sqlc.narg(to_status), @comment, sqlc.narg(user_id));

-- name: AssetEvents :many
select e.id, e.kind, e.at, e.comment, e.from_status, e.to_status,
       fl.name as from_location_name, tl.name as to_location_name,
       fu.full_name as from_user_name, tu.full_name as to_user_name, u.full_name as user_name
from asset_events e
left join locations fl on fl.id = e.from_location_id
left join locations tl on tl.id = e.to_location_id
left join users fu on fu.id = e.from_user_id
left join users tu on tu.id = e.to_user_id
left join users u on u.id = e.user_id
where e.asset_id = $1
order by e.id desc;

-- name: Responsibles :many
-- Кому можно передать имущество под ответственность: сотрудники точки и сотрудники офиса сети.
select u.id, u.full_name, u.role
from users u
where not u.is_blocked
  and (u.role in ('manager', 'accountant')
       or exists (select 1 from user_locations ul where ul.user_id = u.id and ul.location_id = @location_id))
order by u.full_name;

-- name: UserActive :one
select exists (select 1 from users where id = $1 and not is_blocked)::boolean;

-- name: InsertWriteoff :one
insert into asset_writeoffs (asset_id, reason, requested_by) values (@asset_id, @reason, @requested_by) returning id;

-- name: PendingWriteoff :one
select w.id, w.reason, w.requested_by, w.requested_at, u.full_name as requested_by_name
from asset_writeoffs w
join users u on u.id = w.requested_by
where w.asset_id = $1 and w.status = 'pending';

-- name: DecideWriteoff :exec
update asset_writeoffs set status = @status, decided_by = @decided_by, decided_at = now(), decision_comment = @comment
where id = @id;

-- name: LastWriteoff :one
select w.id, w.reason, w.status, w.requested_at, w.decided_at, w.decision_comment,
       ru.full_name as requested_by_name, du.full_name as decided_by_name
from asset_writeoffs w
join users ru on ru.id = w.requested_by
left join users du on du.id = w.decided_by
where w.asset_id = $1
order by w.requested_at desc
limit 1;

-- name: CreateAudit :one
insert into asset_audits (location_id, started_by) values (@location_id, @started_by) returning id;

-- name: OpenAudit :one
select id from asset_audits where location_id = $1 and status = 'counting';

-- name: GetAudit :one
select a.id, a.location_id, l.name as location_name, a.status, a.started_at, a.finished_at,
       su.full_name as started_by_name, fu.full_name as finished_by_name
from asset_audits a
join locations l on l.id = a.location_id
join users su on su.id = a.started_by
left join users fu on fu.id = a.finished_by
where a.id = $1;

-- name: LockAudit :one
select id, location_id, status from asset_audits where id = $1 for update;

-- name: AddAuditScan :execrows
insert into asset_audit_scans (audit_id, asset_id, scanned_by) values (@audit_id, @asset_id, @scanned_by)
on conflict do nothing;

-- name: AuditResults :many
-- Результат по каждому объекту (ИМ-4): числится в точке и найден, не найден — или найден здесь,
-- хотя числится в другой точке.
select a.id, a.number, a.name, c.name as category_name, l.name as location_name, u.full_name as responsible_name,
       (s.asset_id is not null)::boolean as found, (a.location_id <> au.location_id)::boolean as elsewhere,
       s.scanned_at
from asset_audits au
join assets a on (a.location_id = au.location_id and a.status <> 'written_off')
              or a.id in (select asset_id from asset_audit_scans where audit_id = au.id)
join asset_categories c on c.code = a.category_code
join locations l on l.id = a.location_id
left join users u on u.id = a.responsible_id
left join asset_audit_scans s on s.audit_id = au.id and s.asset_id = a.id
where au.id = $1
order by found, a.number;

-- name: FinishAudit :exec
update asset_audits set status = @status, finished_by = @finished_by, finished_at = now() where id = @id;

-- name: ListAudits :many
select a.id, a.location_id, l.name as location_name, a.status, a.started_at, a.finished_at,
       su.full_name as started_by_name
from asset_audits a
join locations l on l.id = a.location_id
join users su on su.id = a.started_by
where (sqlc.narg(location_ids)::uuid[] is null or a.location_id = any(sqlc.narg(location_ids)::uuid[]))
order by a.started_at desc
limit 50;

-- name: OrganizationName :one
select coalesce((select value from app_settings where key = 'organization_name'), 'ООО «Чекушка»')::text;
