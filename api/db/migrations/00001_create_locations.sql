-- +goose Up
create table locations (
    id         uuid primary key default uuidv7(),
    code       text not null unique,
    name       text not null,
    kind       text not null check (kind in ('warehouse', 'store')),
    city       text not null,
    timezone   text not null,
    created_at timestamptz not null default now()
);
