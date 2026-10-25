-- Брони покупателей (НЛ-4, НЛ-5).

-- name: NextReservationNumber :one
insert into doc_counters (prefix, year, last_no)
values ('БР', @year, 1)
on conflict (prefix, year) do update set last_no = doc_counters.last_no + 1
returning last_no;

-- name: ReservationPlace :one
-- Точка брони: местная дата и срок — конец следующего дня по местному времени.
select l.id, l.is_active, l.kind, (now() at time zone l.timezone)::date as local_date,
       ((date_trunc('day', now() at time zone l.timezone) + interval '2 day') at time zone l.timezone)::timestamptz as expires_at
from locations l
where l.id = @id;

-- name: CreateReservation :one
insert into reservations (number, location_id, variant_id, qty, customer_name, customer_phone, consent, expires_at,
                          created_by, comment)
values (@number, @location_id, @variant_id, @qty, @customer_name, @customer_phone, @consent, @expires_at,
        @created_by, @comment)
returning id;

-- name: ListReservations :many
select r.id, r.number, r.location_id, l.code as location_code, l.name as location_name, r.variant_id, v.article,
       p.name, v.volume_ml, v.net_weight_g, v.container, v.gift_pack, k.egais_accounting, r.qty, r.customer_name,
       r.customer_phone, r.status, r.expires_at, z.kind as zone_kind, r.comment, r.created_at, u.full_name as created_by_name,
       coalesce((select array_agg(m.mark_code order by m.mark_code) from reservation_marks m where m.reservation_id = r.id),
                '{}')::text[] as marks,
       pr.number as receipt_number
from reservations r
join locations l on l.id = r.location_id
join variants v on v.id = r.variant_id
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
left join zones z on z.id = r.zone_id
join users u on u.id = r.created_by
left join pos_receipts pr on pr.id = r.receipt_id
where (sqlc.narg(id)::uuid is null or r.id = sqlc.narg(id)::uuid)
  and (sqlc.narg(location_ids)::uuid[] is null or r.location_id = any(sqlc.narg(location_ids)::uuid[]))
  and (sqlc.narg(location_id)::uuid is null or r.location_id = sqlc.narg(location_id)::uuid)
  and (not @open_only::boolean or r.status in ('new', 'confirmed'))
order by r.created_at desc
limit 200;

-- name: LockReservation :one
select r.id, r.number, r.location_id, r.variant_id, r.qty, r.status, r.zone_id, k.egais_accounting
from reservations r
join variants v on v.id = r.variant_id
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
where r.id = @id
for update of r;

-- name: ReservationMarks :many
select mark_code from reservation_marks where reservation_id = @reservation_id order by mark_code;

-- name: AddReservationMarks :exec
insert into reservation_marks (reservation_id, mark_code)
select @reservation_id, unnest(@codes::text[]);

-- name: SetReservationStatus :exec
update reservations
set status = @status, zone_id = coalesce(sqlc.narg(zone_id), zone_id),
    confirmed_by = coalesce(sqlc.narg(confirmed_by), confirmed_by),
    receipt_id = coalesce(sqlc.narg(receipt_id), receipt_id),
    closed_at = case when @status::text in ('issued', 'cancelled', 'expired') then now() else closed_at end
where id = @id;

-- name: FreeZone :one
-- Зона точки, где свободного товара хватает на бронь: сначала торговый зал, потом склад.
select b.zone_id
from stock_balances b
join zones z on z.id = b.zone_id
where b.variant_id = @variant_id and b.location_id = @location_id and b.on_hand - b.reserved >= @qty
order by (z.kind = 'floor') desc
limit 1;

-- name: ChangeReserved :execrows
-- Резерв не больше свободного остатка: ограничение reserved <= on_hand страхует и на уровне БД.
update stock_balances
set reserved = reserved + @delta::integer, updated_at = now()
where variant_id = @variant_id and zone_id = @zone_id and reserved + @delta::integer >= 0
  and (@delta::integer <= 0 or on_hand - reserved >= @delta::integer);

-- name: ExpiredReservations :many
select id from reservations where status in ('new', 'confirmed') and expires_at < now() order by expires_at limit 100;

-- name: AnonymizeReservations :execrows
-- Персональные данные покупателя обезличиваются через 30 дней после закрытия брони (НЛ-5).
update reservations
set customer_name = 'Покупатель', customer_phone = '', anonymized_at = now()
where closed_at < now() - interval '30 days' and anonymized_at is null;

-- name: VariantForReservation :one
select v.id, v.is_active, p.is_active as product_active, k.egais_accounting
from variants v
join products p on p.id = v.product_id
left join product_kinds k on k.code = p.kind_code
where v.id = @id;
