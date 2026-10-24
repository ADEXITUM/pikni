-- Помарочный учёт: статус и место каждой марки ЕГАИС и её история (СО-8).

-- name: LockMarks :many
-- Порядок по коду: два документа с общими марками не заблокируют друг друга.
select code, variant_id, location_id, zone_id, status
from excise_marks
where code = any(@codes::text[])
order by code
for update;

-- name: CreateMarks :exec
insert into excise_marks (code, variant_id, location_id, zone_id, status, last_document_id)
select t.code, t.variant_id, @location_id, @zone_id, 'in_stock', @document_id
from (select unnest(@codes::text[]) as code, unnest(@variant_ids::uuid[]) as variant_id) as t;

-- name: MoveMarks :exec
update excise_marks
set location_id = @location_id, zone_id = sqlc.narg(zone_id), status = @status,
    last_document_id = @document_id, updated_at = now()
where code = any(@codes::text[]);

-- name: InsertMarkEvents :exec
insert into mark_events (mark_code, document_id, user_id, from_status, to_status, from_zone_id, to_zone_id)
select t.code, @document_id, @user_id, nullif(t.from_status, ''), @to_status,
       nullif(t.from_zone_id, '00000000-0000-0000-0000-000000000000'::uuid), sqlc.narg(to_zone_id)
from (select unnest(@codes::text[]) as code, unnest(@from_statuses::text[]) as from_status,
             unnest(@from_zone_ids::uuid[]) as from_zone_id) as t;

-- name: FindMark :one
select code, variant_id, location_id, zone_id, status
from excise_marks
where code = @code;

-- name: GetMark :one
select e.code, e.variant_id, v.product_id, v.article, p.name, e.status, e.location_id, l.name as location_name,
       z.kind as zone_kind
from excise_marks e
join variants v on v.id = e.variant_id
join products p on p.id = v.product_id
join locations l on l.id = e.location_id
left join zones z on z.id = e.zone_id
where e.code = @code;

-- name: ListMarkEvents :many
select m.id, m.at, m.document_id, d.number as document_number, d.kind as document_kind, m.from_status, m.to_status,
       fz.kind as from_zone_kind, tz.kind as to_zone_kind, l.name as location_name, u.full_name as user_name
from mark_events m
left join documents d on d.id = m.document_id
left join zones fz on fz.id = m.from_zone_id
left join zones tz on tz.id = m.to_zone_id
left join locations l on l.id = coalesce(tz.location_id, fz.location_id, d.location_id)
left join users u on u.id = m.user_id
where m.mark_code = @code
order by m.id;
