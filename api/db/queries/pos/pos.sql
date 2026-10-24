-- Учебная касса: кассы, сессии кассиров, смены, чеки (КС-1 – КС-6).

-- name: CreateDevice :one
insert into pos_devices (location_id, name, key_prefix, key_hash, created_by)
values (@location_id, @name, @key_prefix, @key_hash, sqlc.narg(created_by))
returning id;

-- name: DeviceLocation :one
select kind, is_active from locations where id = @id;

-- name: ListDevices :many
select d.id, d.location_id, l.code as location_code, l.name as location_name, d.name, d.key_prefix, d.created_at,
       d.last_seen_at, d.revoked_at
from pos_devices d
join locations l on l.id = d.location_id
where (sqlc.narg(id)::uuid is null or d.id = sqlc.narg(id)::uuid)
  and (sqlc.narg(location_ids)::uuid[] is null or d.location_id = any(sqlc.narg(location_ids)::uuid[]))
order by l.code, d.name;

-- name: DeviceLocationID :one
select location_id from pos_devices where id = @id;

-- name: RevokeDevice :one
update pos_devices set revoked_at = now() where id = @id and revoked_at is null returning name;

-- name: DeviceByPrefix :one
select d.id, d.name, d.key_hash, d.revoked_at, d.location_id, l.code as location_code, l.name as location_name,
       l.timezone, l.fsrar_id, l.is_active as location_active,
       (select z.id from zones z where z.location_id = l.id and z.kind = 'floor')::uuid as floor_zone_id
from pos_devices d
join locations l on l.id = d.location_id
where d.key_prefix = @key_prefix;

-- name: TouchDevice :exec
update pos_devices set last_seen_at = now() where id = @id;

-- name: CashierByLogin :one
select u.id, u.full_name, u.role, u.pin_hash, u.is_blocked, u.failed_logins, u.locked_until,
       coalesce(array_agg(ul.location_id) filter (where ul.location_id is not null), '{}')::uuid[] as location_ids
from users u
left join user_locations ul on ul.user_id = u.id
where lower(u.login) = lower(@login)
group by u.id;

-- name: PinFailed :exec
update users
set failed_logins = failed_logins + 1,
    locked_until = case when failed_logins + 1 >= 5 then now() + interval '15 minutes' else locked_until end
where id = @id;

-- name: PinSucceeded :exec
update users set failed_logins = 0, locked_until = null where id = @id;

-- name: SetPIN :one
update users set pin_hash = @pin_hash, version = version + 1, updated_at = now() where id = @id returning login;

-- name: CreatePosSession :exec
insert into pos_sessions (device_id, user_id, token_hash, expires_at)
values (@device_id, @user_id, @token_hash, @expires_at);

-- name: PosSessionByToken :one
select s.id, s.device_id, s.user_id, u.full_name, u.is_blocked, s.expires_at, s.revoked_at
from pos_sessions s
join users u on u.id = s.user_id
where s.token_hash = @token_hash;

-- name: RevokePosSession :exec
update pos_sessions set revoked_at = now() where id = @id;

-- name: CurrentShift :one
select id, number, sale_document_id, cashier_id, opened_at
from pos_shifts
where device_id = @device_id and status = 'open';

-- name: LockCurrentShift :one
select id, number, sale_document_id, cashier_id, opened_at
from pos_shifts
where device_id = @device_id and status = 'open'
for update;

-- name: NextShiftNumber :one
select (coalesce(max(number), 0) + 1)::integer from pos_shifts where device_id = @device_id;

-- name: CreateShift :one
insert into pos_shifts (device_id, cashier_id, number, sale_document_id)
values (@device_id, @cashier_id, @number, @sale_document_id)
returning id, opened_at;

-- name: CloseShift :one
update pos_shifts set status = @status, closed_at = now() where id = @id returning closed_at;

-- name: ShiftTotals :one
select count(*) filter (where kind = 'sale')::integer as receipts,
       count(*) filter (where kind = 'return')::integer as returns,
       coalesce((sum(case when kind = 'sale' then total else -total end) filter (where payment = 'cash')) * 100, 0)::bigint as cash_kopecks,
       coalesce((sum(case when kind = 'sale' then total else -total end) filter (where payment = 'card')) * 100, 0)::bigint as card_kopecks
from pos_receipts
where shift_id = @shift_id;

-- name: ShiftQuantitySales :many
-- Проданное за смену пиво и другая продукция без марок: акт списания из зала «Реализация».
select l.variant_id, v.alcocode, sum(l.qty)::integer as qty
from pos_receipt_lines l
join pos_receipts r on r.id = l.receipt_id
join variants v on v.id = l.variant_id
join products p on p.id = v.product_id
join product_kinds k on k.code = p.kind_code
where r.shift_id = @shift_id and k.egais_accounting = 'quantity'
group by l.variant_id, v.alcocode
having sum(l.qty) > 0;

-- name: NextReceiptNumber :one
select (coalesce(max(number), 0) + 1)::integer from pos_receipts where shift_id = @shift_id;

-- name: InsertReceipt :exec
insert into pos_receipts (id, shift_id, number, kind, return_of, created_on_device_at, total, payment, age_confirmed,
                          egais_qr, offline)
values (@id, @shift_id, @number, @kind, sqlc.narg(return_of), @created_on_device_at, sqlc.arg(total_kopecks)::bigint / 100.0,
        @payment, @age_confirmed, sqlc.narg(egais_qr), @offline);

-- name: InsertReceiptLines :exec
insert into pos_receipt_lines (receipt_id, line_no, variant_id, qty, price, mark_code, crpt_code)
select @receipt_id, t.line_no, t.variant_id, t.qty, t.price_kopecks / 100.0, nullif(t.mark_code, ''), nullif(t.crpt_code, '')
from (select unnest(@line_nos::integer[]) as line_no, unnest(@variant_ids::uuid[]) as variant_id,
             unnest(@qtys::integer[]) as qty, unnest(@prices_kopecks::bigint[]) as price_kopecks,
             unnest(@mark_codes::text[]) as mark_code, unnest(@crpt_codes::text[]) as crpt_code) as t;

-- name: GetReceipt :one
select r.id, r.number, r.kind, s.number as shift_number, s.device_id, (r.total * 100)::bigint as total_kopecks,
       r.payment, r.egais_qr, r.received_at, r.return_of
from pos_receipts r
join pos_shifts s on s.id = r.shift_id
where r.id = @id;

-- name: ReceiptLines :many
select l.variant_id, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack, l.qty,
       (l.price * 100)::bigint as price_kopecks, l.mark_code, l.crpt_code
from pos_receipt_lines l
join variants v on v.id = l.variant_id
join products p on p.id = v.product_id
where l.receipt_id = @receipt_id
order by l.line_no;

-- name: FindReceipts :many
-- Чеки продажи точки для возврата: по номеру или по марке, новые сверху.
select r.id
from pos_receipts r
join pos_shifts s on s.id = r.shift_id
join pos_devices d on d.id = s.device_id
where d.location_id = @location_id and r.kind = 'sale'
  and (sqlc.narg(number)::integer is null or r.number = sqlc.narg(number)::integer)
  and (sqlc.narg(code)::text is null or exists (
        select 1 from pos_receipt_lines l
        where l.receipt_id = r.id and (l.mark_code = sqlc.narg(code)::text or l.crpt_code = sqlc.narg(code)::text)))
order by r.received_at desc
limit 20;

-- name: ReturnedQty :many
-- Сколько уже возвращено по строкам чека: вернуть больше проданного нельзя.
select l.variant_id, coalesce(l.mark_code, '')::text as mark_code, (-sum(l.qty))::integer as qty
from pos_receipt_lines l
join pos_receipts r on r.id = l.receipt_id
where r.return_of = @receipt_id
group by l.variant_id, l.mark_code;

-- name: PosCatalog :many
-- Каталог для кассы: действующие варианты, штрихкоды, текущая розничная цена, признаки учёта.
select v.id, v.article, p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack, v.gtin, v.alcocode,
       k.egais_accounting, p.crpt_marked, p.abv, c.alcohol,
       (coalesce(gp.name, pc.name, c.name) = 'Табачная продукция')::boolean as tobacco,
       (c.name in ('Сигареты', 'Папиросы'))::boolean as cigarettes,
       coalesce(array_agg(b.code order by b.code) filter (where b.code is not null), '{}')::text[] as barcodes,
       coalesce((select (pr.price * 100)::bigint from prices pr
                 where pr.variant_id = v.id and pr.kind = 'retail' and pr.valid_from <= now()
                 order by pr.valid_from desc limit 1), 0)::bigint as price_kopecks
from variants v
join products p on p.id = v.product_id
join categories c on c.id = p.category_id
left join categories pc on pc.id = c.parent_id
left join categories gp on gp.id = pc.parent_id
left join product_kinds k on k.code = p.kind_code
left join barcodes b on b.variant_id = v.id
where v.is_active and p.is_active
  and (sqlc.narg(variant_id)::uuid is null or v.id = sqlc.narg(variant_id)::uuid)
group by v.id, p.id, k.egais_accounting, c.alcohol, pc.name, gp.name, c.name
order by p.name, v.volume_ml;

-- name: TobaccoMinPrice :one
-- ЕМЦ табачной продукции на дату по Москве: федеральная величина действует с начала суток по Москве.
select (price * 100)::bigint as kopecks
from tobacco_min_prices
where valid_from <= (sqlc.arg(at)::timestamptz at time zone 'Europe/Moscow')::date
order by valid_from desc
limit 1;
