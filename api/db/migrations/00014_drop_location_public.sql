-- +goose Up
-- Публичная страница наличия вынесена из рамок работы (решение 0028), признак показа точки на ней не нужен.
alter table locations drop column is_public;
