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
insert into documents (kind, number, doc_date, location_id, zone_id, dest_zone_id, dest_location_id, author_id, comment, status,
                       reason_id, reverses_id, transfer_id, scope)
values (@kind, @number, @doc_date, @location_id, @zone_id, sqlc.narg(dest_zone_id), sqlc.narg(dest_location_id), @author_id,
        @comment, coalesce(sqlc.narg(status)::text, 'draft'), sqlc.narg(reason_id), sqlc.narg(reverses_id),
        sqlc.narg(transfer_id), sqlc.narg(scope))
returning id;

-- name: LockDocument :one
select id, kind, number, status, doc_date, location_id, zone_id, dest_zone_id, author_id, version,
       receiving_mode, ttn_id, unmapped, dest_location_id, transfer_id, comment, reason_id, reverses_id, scope
from documents
where id = @id
for update;

-- name: GetDocument :one
select d.id, d.kind, d.number, d.status, d.doc_date, d.location_id, l.name as location_name,
       d.zone_id, z.kind as zone_kind, d.dest_zone_id, dz.kind as dest_zone_kind,
       coalesce(au.full_name, 'ЕГАИС')::text as author_name, pu.full_name as posted_by_name, d.comment, d.created_at, d.posted_at, d.version,
       sp.name as supplier_name, d.ttn_number, d.receiving_mode, d.unmapped,
       (select count(*) from document_lines dl where dl.document_id = d.id and dl.qty > 0)::integer as line_count,
       (select coalesce(sum(dl.qty), 0) from document_lines dl where dl.document_id = d.id)::integer as total_qty,
       d.dest_location_id, dl.name as dest_location_name, d.transfer_id, tr.number as transfer_number,
       rc.id as receipt_id, rc.number as receipt_number,
       d.reason_id, wr.name as reason_name, wr.photo_required, d.reverses_id, rv.number as reverses_number,
       rv.kind as reverses_kind, sn.id as storno_id, sn.number as storno_number,
       (select count(*) from document_photos ph where ph.document_id = d.id)::integer as photo_count
from documents d
join locations l on l.id = d.location_id
join zones z on z.id = d.zone_id
left join zones dz on dz.id = d.dest_zone_id
left join locations dl on dl.id = d.dest_location_id
left join documents tr on tr.id = d.transfer_id
left join documents rc on rc.transfer_id = d.id and rc.kind = 'receipt'
left join writeoff_reasons wr on wr.id = d.reason_id
left join documents rv on rv.id = d.reverses_id
left join documents sn on sn.reverses_id = d.id and sn.status <> 'cancelled'
left join users au on au.id = d.author_id
left join suppliers sp on sp.id = d.supplier_id
left join users pu on pu.id = d.posted_by
where d.id = @id;

-- name: ListDocuments :many
-- Видны виды документов, на которые есть право во всей сети (kinds_all) или в своих точках (kinds_own).
-- id — UUIDv7, поэтому порядок по нему — порядок создания.
select d.id, d.kind, d.number, d.status, d.doc_date, d.location_id, l.name as location_name,
       d.zone_id, z.kind as zone_kind, d.dest_zone_id, dz.kind as dest_zone_kind,
       coalesce(au.full_name, 'ЕГАИС')::text as author_name, pu.full_name as posted_by_name, d.created_at, d.posted_at, d.version,
       sp.name as supplier_name, d.ttn_number, d.receiving_mode,
       (select count(*) from document_lines dl where dl.document_id = d.id and dl.qty > 0)::integer as line_count,
       (select coalesce(sum(dl.qty), 0) from document_lines dl where dl.document_id = d.id)::integer as total_qty,
       d.dest_location_id, dl.name as dest_location_name
from documents d
join locations l on l.id = d.location_id
join zones z on z.id = d.zone_id
left join zones dz on dz.id = d.dest_zone_id
left join locations dl on dl.id = d.dest_location_id
left join users au on au.id = d.author_id
left join suppliers sp on sp.id = d.supplier_id
left join users pu on pu.id = d.posted_by
where (d.kind = any(@kinds_all::text[])
       or (d.kind = any(@kinds_own::text[])
           and (d.location_id = any(@own_locations::uuid[]) or d.dest_location_id = any(@own_locations::uuid[]))))
  and (sqlc.narg(kind)::text is null or d.kind = sqlc.narg(kind)::text)
  and (sqlc.narg(status)::text is null or d.status = sqlc.narg(status)::text)
  and (sqlc.narg(location_id)::uuid is null or d.location_id = sqlc.narg(location_id)::uuid
       or d.dest_location_id = sqlc.narg(location_id)::uuid)
  and (sqlc.narg(after)::uuid is null or d.id < sqlc.narg(after)::uuid)
order by d.id desc
limit @max_rows;

-- name: ListLines :many
select dl.id, dl.variant_id, v.product_id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack,
       k.egais_accounting, v.alcocode, dl.qty, dl.qty_plan, dl.ttn_identity,
       coalesce((dl.price * 100)::bigint, 0)::bigint as price_kopecks, (dl.unit_cost is not null)::boolean as costed,
       coalesce((dl.unit_cost * 10000)::bigint, 0)::bigint as unit_cost_e4
from document_lines dl
join variants v on v.id = dl.variant_id
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
where dl.document_id = @document_id
  -- одна строка — и с нулевым количеством: после отмены скана экран должен увидеть ноль
  -- строки ТТН видны и с нулевым фактом: план нужен на экране приёмки
  and (sqlc.narg(variant_id)::uuid is null and (dl.qty > 0 or dl.qty_plan is not null)
       or dl.variant_id = sqlc.narg(variant_id)::uuid)
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
insert into scans (id, document_id, user_id, source, raw, code_kind, variant_id, mark_code, crpt_code, package_code,
                   qty, status, reason, scanned_at, zone_id)
values (@id, @document_id, @user_id, @source, @raw, sqlc.narg(code_kind), sqlc.narg(variant_id),
        sqlc.narg(mark_code), sqlc.narg(crpt_code), sqlc.narg(package_code), @qty, @status, sqlc.narg(reason), @scanned_at,
        sqlc.narg(zone_id));

-- name: UndoScan :one
update scans
set undone_at = now()
where id = @id and document_id = @document_id and undone_at is null
returning variant_id, qty, status, mark_code, package_code, zone_id;

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

-- ---------- поступление по ТТН

-- name: LocationByFSRAR :one
select l.id, l.kind, l.is_active, (now() at time zone l.timezone)::date as local_date,
       (select z.id from zones z where z.location_id = l.id and z.kind in ('warehouse', 'backroom') limit 1)::uuid as zone_id
from locations l
where l.fsrar_id = @fsrar_id;

-- name: SupplierByFSRAR :one
select id, default_receiving_mode
from suppliers
where fsrar_id = @fsrar_id;

-- name: CreateReceipt :one
insert into documents (kind, number, doc_date, location_id, zone_id, supplier_id, receiving_mode, ttn_id, ttn_number,
                       ttn_date, unmapped)
values ('receipt', @number, @doc_date, @location_id, @zone_id, sqlc.narg(supplier_id), @receiving_mode, @ttn_id,
        @ttn_number, sqlc.narg(ttn_date), @unmapped)
on conflict (location_id, ttn_id) where ttn_id is not null do nothing
returning id;

-- name: InsertPlanLine :exec
insert into document_lines (document_id, line_no, variant_id, qty, qty_plan, price, ttn_identity)
values (@document_id, @line_no, @variant_id, 0, @qty_plan, sqlc.arg(price_kopecks)::bigint / 100.0, @ttn_identity)
on conflict (document_id, variant_id) do update
set qty_plan = document_lines.qty_plan + excluded.qty_plan;

-- name: InsertTTNMarks :exec
insert into ttn_marks (document_id, code, variant_id, package_code)
select @document_id, t.code, t.variant_id, nullif(t.package_code, '')
from (select unnest(@codes::text[]) as code, unnest(@variant_ids::uuid[]) as variant_id,
             unnest(@package_codes::text[]) as package_code) as t
on conflict do nothing;

-- name: InsertPackage :exec
insert into document_packages (document_id, code, variant_id, qty)
values (@document_id, @code, sqlc.narg(variant_id), @qty)
on conflict do nothing;

-- name: ReceiptMarks :many
select code, variant_id, package_code
from ttn_marks
where document_id = @document_id;

-- name: ReceiptPackages :many
select code, variant_id, qty, to_open
from document_packages
where document_id = @document_id
order by code;

-- name: AcceptedScans :many
-- Засчитанные сканы документа: марки, коды «Честного знака» и короба — из них считается факт приёмки.
select mark_code, package_code, variant_id, qty
from scans
where document_id = @document_id and status = 'accepted' and undone_at is null;

-- name: SetReceivingMode :exec
update documents set receiving_mode = @receiving_mode where id = @id;

-- name: ResetPackagesToOpen :exec
update document_packages set to_open = false where document_id = @document_id;

-- name: MarkPackagesToOpen :exec
update document_packages set to_open = true where document_id = @document_id and code = any(@codes::text[]);

-- name: CountForeign :one
select count(*)::integer
from scans
where document_id = @document_id and status = 'foreign' and undone_at is null;

-- ---------- перемещение между точками

-- name: StorageZone :one
-- Склад точки: у распределительного склада — склад, у магазина — склад магазина.
select z.id, l.kind as location_kind, l.is_active as location_active, l.fsrar_id, l.name,
       (now() at time zone l.timezone)::date as local_date
from zones z
join locations l on l.id = z.location_id
where z.location_id = @location_id and z.kind in ('warehouse', 'backroom');

-- name: InsertRequestLine :exec
insert into document_lines (document_id, line_no, variant_id, qty, qty_plan)
values (@document_id, @line_no, @variant_id, 0, @qty_plan);

-- name: SetComment :exec
update documents set comment = @comment where id = @id;

-- name: TransferByTTN :one
-- Перемещение, по которому отправлена ТТН: её идентификатор в ЕГАИС — ответ УТМ на отправку.
select m.document_id::uuid
from egais_messages m
join documents d on d.id = m.document_id
where m.direction = 'out' and m.doc_type = 'WayBill' and m.external_id = @external_id and d.kind = 'transfer';

-- name: SetTransfer :exec
update documents set transfer_id = @transfer_id where id = @id;

-- name: ReceiptForTransfer :one
select id from documents where transfer_id = @transfer_id and kind = 'receipt';

-- name: SetReceiptTTN :exec
update documents set ttn_id = @ttn_id where id = @id;

-- ---------- списание, утверждение, сторно

-- name: GetWriteoffReason :one
select id, name, egais_type, photo_required, is_active from writeoff_reasons where id = @id;

-- name: InsertApproval :exec
insert into document_approvals (document_id, user_id, decision, comment)
values (@document_id, @user_id, @decision, @comment);

-- name: InsertPhoto :one
insert into document_photos (document_id, content_type, data, created_by)
values (@document_id, @content_type, @data, @created_by)
returning id, created_at;

-- name: ListPhotos :many
select id, content_type, created_at from document_photos where document_id = @document_id order by created_at;

-- name: GetPhoto :one
select p.id, p.document_id, p.content_type, p.data, d.kind, d.location_id, d.dest_location_id
from document_photos p
join documents d on d.id = p.document_id
where p.id = @id;

-- name: DeletePhoto :execrows
delete from document_photos where id = @id and document_id = @document_id;

-- name: DocumentMoves :many
-- Итог движений документа по корзинам — для сторно.
select variant_id, location_id, zone_id, bucket, sum(qty)::integer as qty
from stock_moves
where document_id = @document_id
group by variant_id, location_id, zone_id, bucket
having sum(qty) <> 0;

-- name: DocumentHasExchange :one
select exists (select 1 from egais_messages where document_id = @document_id);

-- name: TransitLeft :many
-- Что из отгруженного по перемещению так и не принято: остаётся «в пути» у получателя.
select l.variant_id, (coalesce(l.qty_plan, 0) - l.qty)::integer as qty
from document_lines l
join documents r on r.id = l.document_id
where r.transfer_id = @transfer_id and r.kind = 'receipt' and r.status in ('awaiting_egais', 'posted')
  and coalesce(l.qty_plan, 0) > l.qty;

-- name: TransitMarks :many
select code, variant_id from excise_marks where last_document_id = @transfer_id and status = 'in_transit';

-- name: Submitter :one
-- Кто отправил документ на утверждение: он не может сам его утвердить.
select posted_by from documents where id = @id;

-- name: LastApproval :one
select a.decision, a.comment, u.full_name, a.created_at
from document_approvals a
join users u on u.id = a.user_id
where a.document_id = @document_id
order by a.id desc
limit 1;

-- ---------- ревизия (СО-6)

-- name: ActiveStocktake :one
-- В точке идёт одна ревизия за раз: иначе блокировки и снимки учёта перепутались бы.
select number from documents
where kind = 'stocktake' and location_id = @location_id and id <> @id
  and status in ('counting', 'reconciling', 'pending_approval')
limit 1;

-- name: InsertStocktakeZones :exec
insert into stocktake_zones (document_id, zone_id)
select @document_id, unnest(@zone_ids::uuid[]);

-- name: SnapshotBook :exec
-- Учётный остаток в зонах ревизии на момент старта — только товары её состава.
insert into stocktake_book (document_id, zone_id, variant_id, qty, cost_e4)
select d.id, b.zone_id, b.variant_id, b.on_hand, coalesce((c.avg_cost * 10000)::bigint, 0)
from documents d
join stock_balances b on b.location_id = d.location_id
join stocktake_zones z on z.document_id = d.id and z.zone_id = b.zone_id
left join variant_costs c on c.variant_id = b.variant_id
where d.id = @id and b.on_hand > 0
  and stocktake_blocks(d.location_id, b.zone_id, b.variant_id, null) = d.number;

-- name: SnapshotMarks :exec
insert into stocktake_book_marks (document_id, zone_id, mark_code, variant_id)
select d.id, m.zone_id, m.code, m.variant_id
from documents d
join excise_marks m on m.location_id = d.location_id and m.status in ('in_stock', 'reserved')
join stocktake_zones z on z.document_id = d.id and z.zone_id = m.zone_id
where d.id = @id
  and stocktake_blocks(d.location_id, m.zone_id, m.variant_id, null) = d.number;

-- name: InScope :one
-- Товар входит в состав ревизии (её номер совпадает с блокирующей).
select coalesce(stocktake_blocks(d.location_id, @zone_id::uuid, @variant_id::uuid, null) = d.number, false)::boolean
from documents d where d.id = @id;

-- name: StocktakeZones :many
select z.zone_id, zn.kind, z.status, z.closed_at, u.full_name as closed_by_name,
       (select count(*) from stocktake_book_marks bm where bm.document_id = z.document_id and bm.zone_id = z.zone_id)::integer as book_marks,
       (select count(*) from stocktake_marks sm where sm.document_id = z.document_id and sm.zone_id = z.zone_id)::integer as found_marks,
       (select coalesce(sum(b.qty), 0) from stocktake_book b
         join variants v on v.id = b.variant_id join products p on p.id = v.product_id
         left join product_kinds k on k.code = p.kind_code
         where b.document_id = z.document_id and b.zone_id = z.zone_id
           and coalesce(k.egais_accounting, '') <> 'marked')::integer as book_qty,
       (select coalesce(sum(c.qty), 0) from stocktake_counts c
         where c.document_id = z.document_id and c.zone_id = z.zone_id)::integer as counted_qty
from stocktake_zones z
join zones zn on zn.id = z.zone_id
left join users u on u.id = z.closed_by
where z.document_id = @document_id
order by zn.kind desc;

-- name: LockStocktakeZone :one
select status from stocktake_zones where document_id = @document_id and zone_id = @zone_id for update;

-- name: SetStocktakeZone :exec
update stocktake_zones
set status = @status, closed_by = sqlc.narg(closed_by),
    closed_at = case when @status::text = 'closed' then now() end
where document_id = @document_id and zone_id = @zone_id;

-- name: ClearZoneCounts :exec
delete from stocktake_counts where document_id = @document_id and zone_id = @zone_id;

-- name: ClearZoneMarks :exec
delete from stocktake_marks where document_id = @document_id and zone_id = @zone_id;

-- name: UndoZoneScans :exec
update scans set undone_at = now()
where document_id = @document_id and zone_id = @zone_id and undone_at is null;

-- name: AddCount :exec
insert into stocktake_counts (document_id, zone_id, variant_id, qty)
values (@document_id, @zone_id, @variant_id, greatest(@delta::integer, 0))
on conflict (document_id, zone_id, variant_id) do update
set qty = greatest(stocktake_counts.qty + @delta::integer, 0);

-- name: SetCount :exec
insert into stocktake_counts (document_id, zone_id, variant_id, qty)
values (@document_id, @zone_id, @variant_id, @qty)
on conflict (document_id, zone_id, variant_id) do update set qty = excluded.qty;

-- name: BookMarkZone :one
select zone_id from stocktake_book_marks where document_id = @document_id and mark_code = @mark_code;

-- name: AddFoundMark :execrows
insert into stocktake_marks (document_id, zone_id, mark_code, variant_id, kind)
values (@document_id, @zone_id, @mark_code, sqlc.narg(variant_id), @kind)
on conflict (document_id, mark_code) do nothing;

-- name: RemoveFoundMark :exec
delete from stocktake_marks where document_id = @document_id and mark_code = @mark_code;

-- name: StocktakeSheet :many
-- Сличение по зонам и вариантам: учёт на старте и факт (подсчёт товара без марок или найденные марки).
with fact as (
    select c.zone_id, c.variant_id, c.qty from stocktake_counts c where c.document_id = @id
    union all
    select m.zone_id, m.variant_id, count(*)::integer from stocktake_marks m
    where m.document_id = @id and m.variant_id is not null
      and not (m.kind = 'foreign' and exists (select 1 from excise_marks e where e.code = m.mark_code))
    group by m.zone_id, m.variant_id
),
keys as (
    select f.zone_id, f.variant_id from fact f
    union
    select b.zone_id, b.variant_id from stocktake_book b where b.document_id = @id
)
select k.zone_id, zn.kind as zone_kind, k.variant_id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container,
       v.gift_pack, ke.egais_accounting, v.alcocode,
       coalesce((select b.qty from stocktake_book b where b.document_id = @id and b.zone_id = k.zone_id and b.variant_id = k.variant_id), 0)::integer as book_qty,
       coalesce((select sum(f.qty) from fact f where f.zone_id = k.zone_id and f.variant_id = k.variant_id), 0)::integer as fact_qty,
       coalesce((select b.cost_e4 from stocktake_book b where b.document_id = @id and b.zone_id = k.zone_id and b.variant_id = k.variant_id),
                (select (c.avg_cost * 10000)::bigint from variant_costs c where c.variant_id = k.variant_id), 0)::bigint as cost_e4
from keys k
join zones zn on zn.id = k.zone_id
join variants v on v.id = k.variant_id
join products p on p.id = v.product_id
left join product_kinds ke on ke.code = p.kind_code
order by p.name, v.article, zn.kind;

-- name: NotFoundMarks :many
select bm.mark_code, bm.zone_id, zn.kind as zone_kind, bm.variant_id, v.article, p.name
from stocktake_book_marks bm
join zones zn on zn.id = bm.zone_id
join variants v on v.id = bm.variant_id
join products p on p.id = v.product_id
where bm.document_id = @id
  and not exists (select 1 from stocktake_marks sm where sm.document_id = bm.document_id and sm.mark_code = bm.mark_code)
order by p.name, bm.mark_code;

-- name: FoundMarks :many
select sm.mark_code, sm.zone_id, zn.kind as zone_kind, sm.kind, sm.variant_id, v.article, p.name,
       bm.zone_id as book_zone_id, (e.code is not null)::boolean as known
from stocktake_marks sm
join zones zn on zn.id = sm.zone_id
left join variants v on v.id = sm.variant_id
left join products p on p.id = v.product_id
left join stocktake_book_marks bm on bm.document_id = sm.document_id and bm.mark_code = sm.mark_code
left join excise_marks e on e.code = sm.mark_code
where sm.document_id = @id
order by sm.kind, p.name, sm.mark_code;

-- name: ReplaceLines :exec
-- Строки ревизии для журнала и печати: по варианту — учёт (план) и факт.
delete from document_lines where document_id = @document_id;

-- name: InsertStocktakeLine :exec
insert into document_lines (document_id, line_no, variant_id, qty, qty_plan)
values (@document_id, @line_no, @variant_id, @qty, @qty_plan);

-- name: OrganizationName :one
select coalesce((select value from app_settings where key = 'organization_name'), 'ООО «Чекушка»')::text;
