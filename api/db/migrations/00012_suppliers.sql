-- +goose Up
-- Поставщики (НСИ-4). Филиалы одной организации различаются КПП, поэтому уникальна пара
-- (ИНН, КПП). Режим приёмки по умолчанию подставляется в новый документ поступления (СО-2).
create table suppliers (
    id                     uuid primary key default uuidv7(),
    name                   text not null,
    inn                    text not null check (inn ~ '^([0-9]{10}|[0-9]{12})$'),
    kpp                    text check (kpp ~ '^[0-9]{9}$'),
    fsrar_id               text unique check (fsrar_id ~ '^[0-9]{12}$'),
    license_no             text,
    license_valid_to       date,
    default_receiving_mode text not null default 'piece'
        check (default_receiving_mode in ('document', 'boxes', 'sample', 'piece')),
    contacts               jsonb not null default '{}',
    is_active              boolean not null default true,
    unique nulls not distinct (inn, kpp)
);
