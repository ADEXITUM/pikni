package reports

// Отчёты второй очереди (ОТ-2): XYZ-анализ стабильности спроса и квартальные объёмы продаж и
// закупок по видам продукции в декалитрах.
func init() {
	catalog = append(catalog,
		Def{
			Code: "xyz", Title: "XYZ-анализ",
			Description: "Стабильность спроса по неделям: X — коэффициент вариации до 10 %, Y — до 25 %, Z — выше",
			Params:      []string{"period", "locations", "categories", "brands"},
			Roles:       []string{manager, storeManager}, Heavy: true,
			Columns: []Column{{"Товар", Text, false}, {"Артикул", Text, false}, {"Продано за период", Int, true},
				{"В среднем в неделю", Liters, false}, {"Коэффициент вариации", Percent, false}, {"Класс", Text, false}},
			SQL: catTree + `, weeks as (
	select generate_series(date_trunc('week', @from::date), @to::date, interval '1 week')::date as w
), sales as (
	select v.id, p.name, v.article, date_trunc('week', (r.created_on_device_at at time zone l.timezone))::date as w, sum(rl.qty) as qty
	` + saleLines + `
	where ` + saleFilter + `
	group by 1, 2, 3, 4
), items as (
	select distinct id, name, article from sales
), grid as (
	select i.id, i.name, i.article, coalesce(s.qty, 0) as qty
	from items i cross join weeks w left join sales s on s.id = i.id and s.w = w.w
)
select name, article, sum(qty)::int, round(avg(qty), 2), round(100 * stddev_pop(qty) / nullif(avg(qty), 0), 1),
       case when stddev_pop(qty) / nullif(avg(qty), 0) <= 0.1 then 'X'
            when stddev_pop(qty) / nullif(avg(qty), 0) <= 0.25 then 'Y' else 'Z' end
from grid
group by id, name, article
having sum(qty) > 0
order by 5 nulls last`,
		},
		Def{
			Code: "volumes_by_kind", Title: "Объёмы продаж и закупок по видам продукции",
			Description: "За квартал в декалитрах по кодам видов продукции — для анализа и на случай, если точке понадобится декларация",
			Params:      []string{"period", "locations"},
			Roles:       []string{manager, accountant, storeManager},
			Columns: []Column{{"Вид продукции", Text, false}, {"Продано, шт", Int, true}, {"Продано, дал", Liters, true},
				{"Закуплено, шт", Int, true}, {"Закуплено, дал", Liters, true}},
			SQL: catTree + `
select k.code || ' ' || k.name, coalesce(s.qty, 0)::int, round(coalesce(s.dal, 0), 3), coalesce(b.qty, 0)::int, round(coalesce(b.dal, 0), 3)
from product_kinds k
left join (
	select p.kind_code, sum(rl.qty) as qty, sum(rl.qty * coalesce(v.volume_ml, 0)) / 10000.0 as dal
	` + saleLines + `
	where ` + saleFilter + `
	group by 1
) s on s.kind_code = k.code
left join (
	select p.kind_code, sum(m.qty) as qty, sum(m.qty * coalesce(v.volume_ml, 0)) / 10000.0 as dal
	from stock_moves m
	join documents d on d.id = m.document_id
	join variants v on v.id = m.variant_id
	join products p on p.id = v.product_id
	where d.kind = 'receipt' and d.transfer_id is null and m.bucket = 'on_hand' and m.qty > 0
	  and m.doc_date between @from::date and @to::date and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))
	group by 1
) b on b.kind_code = k.code
where s.qty is not null or b.qty is not null
order by k.code`,
		},
	)
}
