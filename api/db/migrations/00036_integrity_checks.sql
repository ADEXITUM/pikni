-- +goose Up
-- Проверка целостности остатков — фоновой задачей раз в час, а не на каждое открытие «Состояния
-- системы»: на федеральной сети (53 млн марок) она идёт около полутора минут. Страница показывает
-- последний результат и время проверки (решение 0064).
create table integrity_checks (
    id          bigint generated always as identity primary key,
    checked_at  timestamptz not null default now(),
    problems    integer not null,
    duration_ms integer not null
);

grant select, insert on integrity_checks to pikni_app;
grant usage on sequence integrity_checks_id_seq to pikni_app;
