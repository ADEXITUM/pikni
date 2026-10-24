-- Документы движения, их строки и сканы (СО-1).

-- name: NextDocNumber :one
-- Счётчик растёт в транзакции документа: при откате номер не теряется.
insert into doc_counters (prefix, year, last_no)
values (@prefix, @year, 1)
on conflict (prefix, year) do update set last_no = doc_counters.last_no + 1
returning last_no;

-- name: GetZone :one
select z.id, z.kind, z.location_id, l.kind as location_kind, l.is_active as location_active,
       (now() at time zone l.timezone)::date as local_date
from zones z
join locations l on l.id = z.location_id
where z.id = @id;

-- name: LocationZones :many
select z.id, z.kind, l.kind as location_kind, l.is_active as location_active,
       (now() at time zone l.timezone)::date as local_date
from zones z
join locations l on l.id = z.location_id
where z.location_id = @location_id;

-- name: CreateDocument :one
insert into documents (kind, number, doc_date, location_id, zone_id, dest_zone_id, author_id, comment)
values (@kind, @number, @doc_date, @location_id, @zone_id, sqlc.narg(dest_zone_id), @author_id, @comment)
returning id;

-- name: LockDocument :one
select id, kind, number, status, doc_date, location_id, zone_id, dest_zone_id, author_id, version
from documents
where id = @id
for update;

-- name: GetDocument :one
select d.id, d.kind, d.number, d.status, d.doc_date, d.location_id, l.name as location_name,
       d.zone_id, z.kind as zone_kind, d.dest_zone_id, dz.kind as dest_zone_kind,
       au.full_name as author_name, pu.full_name as posted_by_name, d.comment, d.created_at, d.posted_at, d.version,
       (select count(*) from document_lines dl where dl.document_id = d.id and dl.qty > 0)::integer as line_count,
       (select coalesce(sum(dl.qty), 0) from document_lines dl where dl.document_id = d.id)::integer as total_qty
from documents d
join locations l on l.id = d.location_id
join zones z on z.id = d.zone_id
left join zones dz on dz.id = d.dest_zone_id
join users au on au.id = d.author_id
left join users pu on pu.id = d.posted_by
where d.id = @id;

-- name: ListDocuments :many
-- Видны виды документов, на которые есть право во всей сети (kinds_all) или в своих точках (kinds_own).
-- id — UUIDv7, поэтому порядок по нему — порядок создания.
select d.id, d.kind, d.number, d.status, d.doc_date, d.location_id, l.name as location_name,
       d.zone_id, z.kind as zone_kind, d.dest_zone_id, dz.kind as dest_zone_kind,
       au.full_name as author_name, pu.full_name as posted_by_name, d.created_at, d.posted_at, d.version,
       (select count(*) from document_lines dl where dl.document_id = d.id and dl.qty > 0)::integer as line_count,
       (select coalesce(sum(dl.qty), 0) from document_lines dl where dl.document_id = d.id)::integer as total_qty
from documents d
join locations l on l.id = d.location_id
join zones z on z.id = d.zone_id
left join zones dz on dz.id = d.dest_zone_id
join users au on au.id = d.author_id
left join users pu on pu.id = d.posted_by
where (d.kind = any(@kinds_all::text[])
       or (d.kind = any(@kinds_own::text[]) and d.location_id = any(@own_locations::uuid[])))
  and (sqlc.narg(kind)::text is null or d.kind = sqlc.narg(kind)::text)
  and (sqlc.narg(status)::text is null or d.status = sqlc.narg(status)::text)
  and (sqlc.narg(location_id)::uuid is null or d.location_id = sqlc.narg(location_id)::uuid)
  and (sqlc.narg(after)::uuid is null or d.id < sqlc.narg(after)::uuid)
order by d.id desc
limit @max_rows;

-- name: ListLines :many
select dl.id, dl.variant_id, v.product_id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       k.egais_accounting, v.alcocode, dl.qty, (dl.unit_cost is not null)::boolean as costed,
       coalesce((dl.unit_cost * 10000)::bigint, 0)::bigint as unit_cost_e4
from document_lines dl
join variants v on v.id = dl.variant_id
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
where dl.document_id = @document_id
  -- одна строка — и с нулевым количеством: после отмены скана экран должен увидеть ноль
  and (sqlc.narg(variant_id)::uuid is null and dl.qty > 0 or dl.variant_id = sqlc.narg(variant_id)::uuid)
order by dl.line_no;

-- name: AddToLine :one
-- Строки нумеруются в порядке первого скана; документ заблокирован, поэтому номер не повторится.
insert into document_lines (document_id, line_no, variant_id, qty)
values (@document_id,
        (select coalesce(max(x.line_no), 0) + 1 from document_lines x where x.document_id = @document_id),
        @variant_id, @qty)
on conflict (document_id, variant_id) do update set qty = document_lines.qty + excluded.qty
returning id;

-- name: SetLineQty :execrows
update document_lines
set qty = @qty
where document_id = @document_id and variant_id = @variant_id;

-- name: SetLineCosts :exec
update document_lines dl
set unit_cost = t.cost_e4 / 10000.0
from (select unnest(@ids::uuid[]) as id, unnest(@costs_e4::bigint[]) as cost_e4) as t
where dl.id = t.id;

-- name: VariantInfo :one
-- Сведения о варианте для проверки скана: способ учёта в ЕГАИС и маркировка «Честным знаком».
select v.id, v.product_id, v.article, p.name, k.egais_accounting, p.crpt_marked, (v.is_active and p.is_active)::boolean as is_active
from variants v
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
where v.id = @id;

-- name: GetScan :one
select s.id, s.document_id, s.raw, s.status, s.reason, s.variant_id, v.article, p.name, s.mark_code, s.qty,
       s.scanned_at, s.undone_at
from scans s
left join variants v on v.id = s.variant_id
left join products p on p.id = v.product_id
where s.id = @id;

-- name: CodeScanned :one
-- Марка или код «Честного знака» уже засчитаны в документе.
select exists (
    select 1 from scans
    where document_id = @document_id and status = 'accepted' and undone_at is null
      and (mark_code = sqlc.narg(mark_code)::text or crpt_code = sqlc.narg(crpt_code)::text)
)::boolean;

-- name: InsertScan :exec
insert into scans (id, document_id, user_id, source, raw, code_kind, variant_id, mark_code, crpt_code, qty,
                   status, reason, scanned_at)
values (@id, @document_id, @user_id, @source, @raw, sqlc.narg(code_kind), sqlc.narg(variant_id),
        sqlc.narg(mark_code), sqlc.narg(crpt_code), @qty, @status, sqlc.narg(reason), @scanned_at);

-- name: UndoScan :one
update scans
set undone_at = now()
where id = @id and document_id = @document_id and undone_at is null
returning variant_id, qty, status;

-- name: ListScans :many
select s.id, s.raw, s.status, s.reason, s.variant_id, v.article, p.name, s.mark_code, s.qty, s.scanned_at, s.undone_at
from scans s
left join variants v on v.id = s.variant_id
left join products p on p.id = v.product_id
where s.document_id = @document_id
order by s.received_at desc, s.id desc
limit 50;

-- name: DocumentMarks :many
select mark_code::text as code, variant_id
from scans
where document_id = @document_id and mark_code is not null and status = 'accepted' and undone_at is null
order by mark_code;

-- name: SetDocumentStatus :exec
update documents
set status = @status, posted_by = coalesce(sqlc.narg(posted_by), posted_by),
    posted_at = case when @status::text in ('posted', 'awaiting_egais') then now() else posted_at end,
    version = version + 1
where id = @id;

-- name: ChangeLineQty :exec
-- Отмена скана уменьшает строку отдельным запросом: вставка с отрицательным количеством
-- нарушила бы ограничение раньше, чем сработает on conflict.
update document_lines
set qty = qty + @delta::integer
where document_id = @document_id and variant_id = @variant_id;
