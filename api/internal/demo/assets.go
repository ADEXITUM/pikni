package demo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Assets ставит на учёт оборудование точек, где имущества ещё нет: холодильники, стеллажи, кассы,
// ТСД, сейфы. Набор зависит от точки: крупный магазин, магазин у дома (матрица меньше тысячи
// позиций) или склад. Ответственный — управляющий магазина или кладовщик склада.
func Assets(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			with tpl (name, category, big, small, wh, cost) as (values
				('Холодильник для пива Polair', 'refrigeration', 4, 2, 0, 85000),
				('Винный шкаф Liebherr', 'refrigeration', 2, 0, 0, 120000),
				('Ларь морозильный для льда', 'refrigeration', 1, 1, 0, 35000),
				('Стеллаж торговый пристенный', 'displays', 18, 8, 0, 22000),
				('Витрина для элитного алкоголя с замком', 'displays', 2, 0, 0, 65000),
				('Стеллаж складской металлический', 'displays', 6, 2, 40, 15000),
				('Онлайн-касса АТОЛ', 'pos', 3, 1, 0, 45000),
				('POS-терминал эквайринга', 'pos', 3, 1, 0, 18000),
				('Сканер штрихкодов 2D', 'pos', 3, 1, 0, 9000),
				('Стол-стойка кассира', 'furniture', 3, 1, 0, 40000),
				('Терминал сбора данных', 'computers', 2, 1, 6, 55000),
				('Принтер этикеток', 'computers', 1, 1, 2, 25000),
				('Компьютер управляющего', 'computers', 1, 1, 1, 60000),
				('Камера видеонаблюдения', 'computers', 6, 3, 8, 7000),
				('Сейф', 'furniture', 1, 1, 1, 30000),
				('Тележка гидравлическая', 'furniture', 0, 0, 3, 28000),
				('Кондиционер', 'furniture', 1, 1, 1, 50000)),
			points as (
				select l.id, l.kind,
				       case when l.kind = 'warehouse' then 'wh'
				            when (select count(*) from assortment_matrix m where m.location_id = l.id) between 1 and 999 then 'small'
				            else 'big' end as size,
				       (select ul.user_id from user_locations ul join users u on u.id = ul.user_id
				        where ul.location_id = l.id and not u.is_blocked
				          and u.role = case when l.kind = 'warehouse' then 'storekeeper' else 'store_manager' end
				        order by u.login limit 1) as responsible
				from locations l
				where l.is_active and l.kind in ('store', 'warehouse')
				  and not exists (select 1 from assets a where a.location_id = l.id)),
			added as (
				insert into assets (number, name, category_code, serial, accepted_at, cost, location_id, responsible_id, status)
				select 'INV-' || lpad(nextval('asset_number_seq')::text, 6, '0'), t.name, t.category,
				       upper(substr(md5(random()::text), 1, 10)),
				       current_date - (30 + floor(random() * 1800))::int,
				       round((t.cost * (0.85 + random() * 0.3))::numeric, -2),
				       p.id, p.responsible,
				       case when random() < 0.03 then 'repair' else 'in_use' end
				from points p
				join tpl t on true
				cross join lateral generate_series(1, case p.size when 'big' then t.big when 'small' then t.small else t.wh end)
				returning id, location_id, status, responsible_id)
			insert into asset_events (asset_id, kind, to_location_id, to_user_id, to_status, comment)
			select id, 'created', location_id, responsible_id, status, 'Поставлено на учёт при открытии точки'
			from added
			returning 1`)
		if err != nil {
			return err
		}
		for rows.Next() {
			n++
		}
		return rows.Err()
	})
	return n, err
}
