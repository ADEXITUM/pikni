-- Ассортиментные матрицы магазинов для демо-каталога; запускать после импорта catalog.csv.
-- Повторный запуск не меняет того, что уже есть в матрицах.
insert into assortment_matrix (location_id, variant_id, min_qty)
select l.id, v.id,
       case
           when c.name = 'Пиво' then 24
           when c.name = 'Водка' and v.volume_ml = 500 then 12
           else 3
       end
from locations l
cross join variants v
join products p on p.id = v.product_id
join categories c on c.id = p.category_id
where l.kind = 'store'
  and v.is_active and p.is_active and not v.gift_pack
  and coalesce(v.volume_ml, 0) <= 1000
  -- В небольшом магазине на 50 лет Октября вина нет.
  and not (l.code = 'МГ-02' and c.name in ('Тихое', 'Игристое', 'Креплёное'))
on conflict (location_id, variant_id) do nothing;
