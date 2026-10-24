-- +goose Up
-- Одна позиция — одна запись: второй «Тайги» у того же производителя быть не может. Повторный
-- импорт находит позицию по этой паре и предлагает изменения, а не создаёт дубль (решение 0033).
create unique index products_producer_name_key on products (producer_id, lower(name));
