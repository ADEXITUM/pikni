-- +goose Up
create table ru_subjects (
    code text primary key check (code ~ '^[0-9]{2}$'),
    name text not null unique
);

insert into ru_subjects (code, name) values
    ('25', 'Приморский край'),
    ('27', 'Хабаровский край'),
    ('28', 'Амурская область');

alter table locations
    add column subject_code     text references ru_subjects (code),
    add column address          text,
    add column lat              double precision check (lat between -90 and 90),
    add column lon              double precision check (lon between -180 and 180),
    add column fsrar_id         text unique check (fsrar_id ~ '^[0-9]{12}$'),
    add column kpp              text check (kpp ~ '^[0-9]{9}$'),
    add column license_no       text,
    add column license_valid_to date,
    add column opening_hours    jsonb not null default '{}',
    add column is_public        boolean not null default false,
    add column is_active        boolean not null default true,
    add column version          integer not null default 1,
    add column updated_at       timestamptz not null default now(),
    add constraint locations_coordinates_check check ((lat is null) = (lon is null));

-- Точки, созданные раньше (начальные данные разработки), получают регион по городу.
update locations
set subject_code = case city
    when 'Благовещенск' then '28'
    when 'Хабаровск' then '27'
    when 'Владивосток' then '25'
end;

alter table locations alter column subject_code set not null;

-- Зоны совпадают с регистрами ЕГАИС: склад — регистр 1, торговый зал — регистр 2.
create table zones (
    id          uuid primary key default uuidv7(),
    location_id uuid not null references locations (id),
    kind        text not null check (kind in ('warehouse', 'backroom', 'floor')),
    name        text not null,
    unique (location_id, kind)
);

-- +goose StatementBegin
create function create_location_zones() returns trigger
language plpgsql as $$
begin
    if new.kind = 'warehouse' then
        insert into zones (location_id, kind, name) values (new.id, 'warehouse', 'Склад');
    else
        insert into zones (location_id, kind, name) values
            (new.id, 'backroom', 'Склад магазина'),
            (new.id, 'floor', 'Торговый зал');
    end if;
    return new;
end;
$$;
-- +goose StatementEnd

create trigger locations_create_zones
    after insert on locations
    for each row execute function create_location_zones();

insert into zones (location_id, kind, name)
select id, 'warehouse', 'Склад' from locations where kind = 'warehouse'
union all
select id, 'backroom', 'Склад магазина' from locations where kind = 'store'
union all
select id, 'floor', 'Торговый зал' from locations where kind = 'store';
