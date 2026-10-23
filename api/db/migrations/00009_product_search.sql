-- +goose Up
-- Поиск по наименованию (НСИ-8): русская морфология («водки» находит «водка») и начало слова
-- («тайг» находит «Тайга»). База пересчитывает колонку сама при каждом изменении позиции.
alter table products
    add column search tsvector generated always as (to_tsvector('russian', name || ' ' || coalesce(variety, ''))) stored;

create index products_search_idx on products using gin (search);
