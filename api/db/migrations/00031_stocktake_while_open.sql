-- +goose Up
-- Ревизия без остановки торговли (решение 0060): продажи во время подсчёта идут, сличение их
-- учитывает. Нужны момент начала подсчёта и момент, когда товар в зоне посчитали последний раз:
-- проданное до этого момента уменьшает учёт, проданное после — и учёт, и посчитанное.
alter table documents add column counting_at timestamptz;
alter table stocktake_counts add column counted_at timestamptz not null default now();

update documents set counting_at = created_at where kind = 'stocktake' and status <> 'draft' and counting_at is null;

-- Остановить продажи можно только явно — флаг strict в составе ревизии (годовая инвентаризация).
-- +goose StatementBegin
create function stocktake_stops_sales(p_number text) returns boolean language sql stable as $$
    select coalesce((select (scope ->> 'strict')::boolean from documents where kind = 'stocktake' and number = p_number), false);
$$;
-- +goose StatementEnd
