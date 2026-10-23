-- +goose Up
-- Ассортиментная матрица (НСИ-9): варианты, которые должны быть в наличии в точке, и минимальный
-- остаток для каждого. Остаток ниже минимального попадёт в уведомления и отчёт «Товары к пополнению».
create table assortment_matrix (
    location_id uuid not null references locations (id),
    variant_id  uuid not null references variants (id),
    min_qty     integer not null default 1 check (min_qty between 1 and 9999),
    primary key (location_id, variant_id)
);

create index assortment_matrix_variant_idx on assortment_matrix (variant_id);
