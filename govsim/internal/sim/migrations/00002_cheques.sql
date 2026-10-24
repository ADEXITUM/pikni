-- +goose Up
-- Чеки с марками (docs/design/govsim.md, п. 3.2): повтор чека с тем же идентификатором возвращает
-- прежний ответ.
create table cheques (
    receipt_id text primary key,
    fsrar_id   text not null references participants,
    kind       text not null check (kind in ('sale', 'return')),
    marks      text[] not null,
    result     text not null check (result in ('accepted', 'rejected')),
    reason     text,
    qr_url     text,
    created_at timestamptz not null default now()
);
