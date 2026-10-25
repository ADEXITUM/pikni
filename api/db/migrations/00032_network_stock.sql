-- +goose Up
-- Остаток по сети для «Наличия» (решение 0064). Сумма по всем точкам на каждый поиск — это все строки
-- остатков: на федеральной сети в 3000 магазинов 10 млн строк и секунды на запрос. Сеть считается
-- заранее и обновляется раз в минуту фоновой задачей; остаток своей точки и выбранных точек — живой.
create materialized view network_stock as
    select variant_id, sum(on_hand)::integer as on_hand
    from stock_balances
    group by variant_id;

create unique index network_stock_variant_idx on network_stock (variant_id);

grant select on network_stock to pikni_app;

-- Обновлять представление может только владелец: функция выполняется с его правами.
-- +goose StatementBegin
create function refresh_network_stock() returns void language plpgsql security definer
set search_path = public as $$
begin
    refresh materialized view concurrently network_stock;
end;
$$;
-- +goose StatementEnd

revoke execute on function refresh_network_stock() from public;
grant execute on function refresh_network_stock() to pikni_app;

