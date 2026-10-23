-- name: ListSaleRules :many
select id, subject_code, scope, valid_from, sale_from, sale_to, source
from alcohol_sale_rules
order by subject_code nulls first, scope, valid_from;

-- name: ListSaleBans :many
select id, subject_code, name, scope, date, month_day, source
from alcohol_sale_bans
order by subject_code, month_day nulls last, date;

-- name: ListSubjectSaleRules :many
select subject_code, scope, valid_from, sale_from, sale_to
from alcohol_sale_rules
where subject_code is null or subject_code = @subject_code;

-- name: ListSubjectSaleBans :many
select name, scope, date, month_day
from alcohol_sale_bans
where subject_code = @subject_code;

-- name: CreateSaleRule :one
insert into alcohol_sale_rules (subject_code, scope, valid_from, sale_from, sale_to, source)
values (sqlc.narg(subject_code), @scope, @valid_from, @sale_from, @sale_to, @source)
returning id, subject_code, scope, valid_from, sale_from, sale_to, source;

-- name: DeleteFutureSaleRule :one
-- Удалить можно только редакцию, которая ещё нигде не вступила в силу: на Камчатке
-- (UTC+12) новый день наступает раньше всех в России.
delete from alcohol_sale_rules
where id = @id
  and valid_from > (now() at time zone 'Asia/Kamchatka')::date
returning subject_code, scope, valid_from;

-- name: SaleRuleExists :one
select exists (select 1 from alcohol_sale_rules where id = @id);

-- name: CreateSaleBan :one
insert into alcohol_sale_bans (subject_code, name, scope, date, month_day, source)
values (@subject_code, @name, @scope, sqlc.narg(date), sqlc.narg(month_day), @source)
returning id, subject_code, name, scope, date, month_day, source;

-- name: DeleteSaleBan :one
delete from alcohol_sale_bans
where id = @id
returning subject_code, name;
