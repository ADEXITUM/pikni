// Package reports — каталог отчётов (ОТ-1, ОТ-2): у каждого отчёта параметры, право и роли, кому
// он доступен, и запрос. Результат — таблица: на экране, в Excel или PDF. Тяжёлые отчёты
// формируются фоновой задачей и ждут в «Моих отчётах» семь дней (ОТ-3).
//
// Запросы написаны вручную, а не через sqlc: у аналитических отчётов много необязательных
// фильтров и группировок, а результат одинаково уходит в общую таблицу.
package reports

import (
	"time"

	"github.com/google/uuid"
)

// Kind — как показывать значение колонки.
type Kind string

const (
	Text    Kind = "text"
	Int     Kind = "int"
	Money   Kind = "money"
	Liters  Kind = "liters"
	Date    Kind = "date"
	Percent Kind = "percent"
)

type Column struct {
	Title string
	Kind  Kind
	// Sum — итог по колонке внизу таблицы.
	Sum bool
}

type Table struct {
	Title   string
	Columns []Column
	Rows    [][]any
}

// Params — параметры отчёта; какие из них нужны, говорит Def.Params.
type Params struct {
	From, To   time.Time
	Date       time.Time
	Locations  []uuid.UUID
	Categories []uuid.UUID
	Brands     []uuid.UUID
	Group      string
	Days       int
}

// Def — отчёт каталога.
type Def struct {
	Code, Title, Description string
	// Params — какие параметры показать в форме: period, date, locations, categories, brands, group, days.
	Params []string
	// Roles — кому доступен отчёт (таблица ОТ-2); точки ограничиваются правом reports.view.
	Roles   []string
	Columns []Column
	SQL     string
	// Heavy — в Excel и PDF формируется в фоне.
	Heavy bool
	// Own — продавец-кассир видит только свои продажи и смены.
	Own bool
}

const (
	manager      = "manager"
	accountant   = "accountant"
	storeManager = "store_manager"
	storekeeper  = "storekeeper"
	cashier      = "cashier"
)

// Общие части запросов: поддерево выбранных категорий и фильтры.
const catTree = `with recursive cat_tree as (
	select id from categories where id = any(coalesce(@cats::uuid[], '{}'))
	union all
	select c.id from categories c join cat_tree t on c.parent_id = t.id
)`

const productFilter = `(@cats::uuid[] is null or p.category_id in (select id from cat_tree))
	and (@brands::uuid[] is null or p.brand_id = any(@brands::uuid[]))`

const zoneName = `case z.kind when 'warehouse' then 'Склад' when 'backroom' then 'Склад магазина' else 'Торговый зал' end`

const retailAt = `coalesce((select pr.price from prices pr where pr.variant_id = v.id and pr.kind = 'retail'
	and pr.valid_from < (@date::date + 1) order by pr.valid_from desc limit 1), 0)`

const available = `coalesce((select sum(b.on_hand - b.reserved) from stock_balances b
	where b.location_id = m.location_id and b.variant_id = m.variant_id), 0)`

const saleLines = `from pos_receipt_lines rl
	join pos_receipts r on r.id = rl.receipt_id
	join pos_shifts s on s.id = r.shift_id
	join pos_devices d on d.id = s.device_id
	join locations l on l.id = d.location_id
	join variants v on v.id = rl.variant_id
	join products p on p.id = v.product_id`

// Первое условие — для индекса по времени чека (с запасом в сутки на часовые пояса), второе — точное.
const saleFilter = `r.created_on_device_at >= @from::date - interval '1 day' and r.created_on_device_at < @to::date + interval '2 days'
	and (r.created_on_device_at at time zone l.timezone)::date between @from::date and @to::date
	and (@locs::uuid[] is null or l.id = any(@locs::uuid[]))
	and (@me::uuid is null or s.cashier_id = @me::uuid)
	and ` + productFilter

var catalog = []Def{
	{
		Code: "stock_on_date", Title: "Ведомость остатков на дату",
		Description: "Количество, объём в литрах, сумма по себестоимости и в розничных ценах по точкам и зонам",
		Params:      []string{"date", "locations", "categories", "brands"},
		Roles:       []string{manager, accountant, storeManager, storekeeper},
		Columns: []Column{{"Точка", Text, false}, {"Зона", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false},
			{"Кол-во", Int, true}, {"Литров", Liters, true}, {"Себестоимость, ₽", Money, true}, {"В розничных ценах, ₽", Money, true}},
		SQL: catTree + `
select l.code || ' · ' || l.name, ` + zoneName + `, p.name, v.article, sum(m.qty)::int,
       round(sum(m.qty) * coalesce(v.volume_ml, 0) / 1000.0, 3), round(sum(m.cost_amount), 2),
       round(sum(m.qty) * ` + retailAt + `, 2)
from stock_moves m
join locations l on l.id = m.location_id
join zones z on z.id = m.zone_id
join variants v on v.id = m.variant_id
join products p on p.id = v.product_id
where m.bucket = 'on_hand' and m.doc_date <= @date::date
  and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[])) and ` + productFilter + `
group by l.code, l.name, z.kind, p.name, v.article, v.volume_ml, v.id
having sum(m.qty) <> 0
order by l.code, z.kind, p.name`,
	},
	{
		Code: "turnover", Title: "Оборотная ведомость",
		Description: "Остаток на начало, приход, расход и остаток на конец периода по точкам",
		Params:      []string{"period", "locations", "categories", "brands"},
		Roles:       []string{manager, accountant, storeManager}, Heavy: true,
		Columns: []Column{{"Точка", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false}, {"На начало", Int, true},
			{"Приход", Int, true}, {"Расход", Int, true}, {"На конец", Int, true}, {"Себестоимость на конец, ₽", Money, true}},
		// Перемещение между зонами одной точки в сумме по документу даёт ноль и в обороты не попадает.
		SQL: catTree + `, per_doc as (
	select m.location_id, m.variant_id, m.document_id, m.doc_date, sum(m.qty) as qty, sum(m.cost_amount) as cost
	from stock_moves m
	where m.bucket = 'on_hand' and m.doc_date <= @to::date and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))
	group by 1, 2, 3, 4
)
select l.code || ' · ' || l.name, p.name, v.article,
       coalesce(sum(d.qty) filter (where d.doc_date < @from::date), 0)::int,
       coalesce(sum(d.qty) filter (where d.doc_date >= @from::date and d.qty > 0), 0)::int,
       coalesce(-sum(d.qty) filter (where d.doc_date >= @from::date and d.qty < 0), 0)::int,
       coalesce(sum(d.qty), 0)::int, round(coalesce(sum(d.cost), 0), 2)
from per_doc d
join locations l on l.id = d.location_id
join variants v on v.id = d.variant_id
join products p on p.id = v.product_id
where ` + productFilter + `
group by l.code, l.name, p.name, v.article
having sum(d.qty) <> 0 or bool_or(d.doc_date >= @from::date and d.qty <> 0)
order by l.code, p.name`,
	},
	{
		Code: "alcohol_sales", Title: "Розничные продажи алкогольной продукции",
		Description: "По каждому магазину: дата, вид продукции, наименование, объём тары, количество, литры и сколько зафиксировано в ЕГАИС",
		Params:      []string{"period", "locations"},
		Roles:       []string{manager, accountant, storeManager}, Heavy: true,
		Columns: []Column{{"Магазин", Text, false}, {"Дата", Date, false}, {"Вид продукции", Text, false}, {"Наименование", Text, false},
			{"Объём тары, л", Liters, false}, {"Кол-во", Int, true}, {"Литров", Liters, true}, {"Зафиксировано в ЕГАИС, шт", Int, true}},
		// Крепкий алкоголь фиксируется чеком с маркой, пиво — актом списания из зала по смене.
		SQL: catTree + `
select l.code || ' · ' || l.name, (r.created_on_device_at at time zone l.timezone)::date, k.code || ' ' || k.name, p.name,
       round(coalesce(v.volume_ml, 0) / 1000.0, 3), sum(rl.qty)::int, round(sum(rl.qty) * coalesce(v.volume_ml, 0) / 1000.0, 3),
       coalesce(sum(rl.qty) filter (where (k.egais_accounting = 'marked' and r.egais_qr is not null)
           or (k.egais_accounting = 'quantity' and exists (select 1 from egais_messages em where em.document_id = s.sale_document_id
               and em.doc_type = 'ActWriteOffShop' and em.status in ('accepted', 'processed')))), 0)::int
` + saleLines + `
join product_kinds k on k.code = p.kind_code
where ` + saleFilter + `
group by l.code, l.name, 2, k.code, k.name, p.name, v.volume_ml
order by l.code, 2, p.name`,
	},
	{
		Code: "sales", Title: "Продажи",
		Description: "Выручка, количество и объём по точкам, категориям, брендам, кассирам или дням",
		Params:      []string{"period", "locations", "categories", "brands", "group"},
		Roles:       []string{manager, accountant, storeManager, cashier}, Heavy: true, Own: true,
		Columns: []Column{{"Группа", Text, false}, {"Чеков", Int, true}, {"Кол-во", Int, true}, {"Литров", Liters, true},
			{"Возвраты, ₽", Money, true}, {"Выручка, ₽", Money, true}},
		SQL: catTree + `
select case @group::text when 'category' then c.name when 'brand' then coalesce(b.name, 'Без бренда')
                        when 'cashier' then u.full_name when 'day' then to_char((r.created_on_device_at at time zone l.timezone)::date, 'DD.MM.YYYY')
                        else l.code || ' · ' || l.name end as grp,
       count(distinct r.id) filter (where r.kind = 'sale'), sum(rl.qty)::int,
       round(sum(rl.qty * coalesce(v.volume_ml, 0)) / 1000.0, 3),
       round(coalesce(-sum(rl.qty * rl.price) filter (where rl.qty < 0), 0), 2), round(sum(rl.qty * rl.price), 2)
` + saleLines + `
join categories c on c.id = p.category_id
left join brands b on b.id = p.brand_id
join users u on u.id = s.cashier_id
where ` + saleFilter + `
group by grp
order by case when @group::text = 'day' then min(r.created_on_device_at)::text else '' end, 6 desc`,
	},
	{
		Code: "shift_summary", Title: "Сводный отчёт смены",
		Description: "Чеки, выручка и возвраты по сменам",
		Params:      []string{"period", "locations"},
		Roles:       []string{storeManager, cashier}, Own: true,
		Columns: []Column{{"Магазин", Text, false}, {"Касса", Text, false}, {"Смена", Int, false}, {"Кассир", Text, false},
			{"Открыта", Text, false}, {"Закрыта", Text, false}, {"Продаж", Int, true}, {"Возвратов", Int, true},
			{"Продажи, ₽", Money, true}, {"Возвраты, ₽", Money, true}},
		SQL: `
select l.code || ' · ' || l.name, d.name, s.number, u.full_name,
       to_char(s.opened_at at time zone l.timezone, 'DD.MM.YYYY HH24:MI'),
       coalesce(to_char(s.closed_at at time zone l.timezone, 'DD.MM.YYYY HH24:MI'), 'открыта'),
       count(r.id) filter (where r.kind = 'sale'), count(r.id) filter (where r.kind = 'return'),
       round(coalesce(sum(r.total) filter (where r.kind = 'sale'), 0), 2), round(coalesce(-sum(r.total) filter (where r.kind = 'return'), 0), 2)
from pos_shifts s
join pos_devices d on d.id = s.device_id
join locations l on l.id = d.location_id
join users u on u.id = s.cashier_id
left join pos_receipts r on r.shift_id = s.id
where (s.opened_at at time zone l.timezone)::date between @from::date and @to::date
  and (@locs::uuid[] is null or l.id = any(@locs::uuid[])) and (@me::uuid is null or s.cashier_id = @me::uuid)
group by l.code, l.name, d.name, s.number, u.full_name, s.opened_at, s.closed_at, l.timezone
order by s.opened_at desc`,
	},
	{
		Code: "abc", Title: "ABC-анализ",
		Description: "Классы A, B, C по выручке и по количеству: A — первые 80 %, B — следующие 15 %, C — остальное",
		Params:      []string{"period", "locations", "categories", "brands"},
		Roles:       []string{manager, storeManager}, Heavy: true,
		Columns: []Column{{"Товар", Text, false}, {"Артикул", Text, false}, {"Кол-во", Int, true}, {"Выручка, ₽", Money, true},
			{"Доля выручки нарастающим итогом", Percent, false}, {"Класс по выручке", Text, false}, {"Класс по количеству", Text, false}},
		SQL: catTree + `, s as (
	select v.id, p.name, v.article, sum(rl.qty) as qty, sum(rl.qty * rl.price) as revenue
	` + saleLines + `
	where ` + saleFilter + `
	group by v.id, p.name, v.article
	having sum(rl.qty) > 0
), r as (
	select s.*, sum(revenue) over (order by revenue desc, id rows unbounded preceding) / nullif(sum(revenue) over (), 0) as cr,
	       sum(qty) over (order by qty desc, id rows unbounded preceding)::numeric / nullif(sum(qty) over (), 0) as cq
	from s
)
select name, article, qty::int, round(revenue, 2), round(cr * 100, 1),
       case when cr <= 0.8 then 'A' when cr <= 0.95 then 'B' else 'C' end,
       case when cq <= 0.8 then 'A' when cq <= 0.95 then 'B' else 'C' end
from r order by revenue desc`,
	},
	{
		Code: "dead_stock", Title: "Неликвид",
		Description: "Товары с остатком без продаж за последние N дней",
		Params:      []string{"locations", "categories", "brands", "days"},
		Roles:       []string{manager, storeManager},
		Columns: []Column{{"Точка", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false}, {"Остаток", Int, true},
			{"Себестоимость, ₽", Money, true}, {"Последняя продажа", Text, false}},
		SQL: catTree + `, stock as (
	select b.location_id, b.variant_id, sum(b.on_hand) as qty from stock_balances b
	where (@locs::uuid[] is null or b.location_id = any(@locs::uuid[])) group by 1, 2 having sum(b.on_hand) > 0
), last_sale as (
	select d.location_id, rl.variant_id, max(r.created_on_device_at) as at
	from pos_receipt_lines rl join pos_receipts r on r.id = rl.receipt_id join pos_shifts s on s.id = r.shift_id
	join pos_devices d on d.id = s.device_id where rl.qty > 0 group by 1, 2
)
select l.code || ' · ' || l.name, p.name, v.article, st.qty::int, round(st.qty * coalesce(c.avg_cost, 0), 2),
       coalesce(to_char(ls.at at time zone l.timezone, 'DD.MM.YYYY'), 'не продавался')
from stock st
join locations l on l.id = st.location_id
join variants v on v.id = st.variant_id
join products p on p.id = v.product_id
left join variant_costs c on c.variant_id = v.id
left join last_sale ls on ls.location_id = st.location_id and ls.variant_id = st.variant_id
where (ls.at is null or ls.at < now() - make_interval(days => @days::int)) and ` + productFilter + `
order by 5 desc`,
	},
	{
		Code: "matrix_gaps", Title: "Разрывы ассортиментной матрицы",
		Description: "Позиции матрицы, которых нет в зале магазина, хотя они есть в сети",
		Params:      []string{"locations", "categories", "brands"},
		Roles:       []string{manager, storeManager},
		Columns: []Column{{"Магазин", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false}, {"Минимум", Int, false},
			{"В сети, шт", Int, false}, {"Где есть", Text, false}},
		SQL: catTree + `
select l.code || ' · ' || l.name, p.name, v.article, m.min_qty,
       coalesce((select sum(b.on_hand - b.reserved) from stock_balances b where b.variant_id = m.variant_id and b.location_id <> m.location_id), 0)::int,
       coalesce((select string_agg(x.code, ', ' order by x.qty desc) from (
           select ol.code, sum(b.on_hand - b.reserved) as qty from stock_balances b join locations ol on ol.id = b.location_id
           where b.variant_id = m.variant_id and b.location_id <> m.location_id group by ol.code having sum(b.on_hand - b.reserved) > 0
           order by 2 desc limit 3) x), '')
from assortment_matrix m
join locations l on l.id = m.location_id and l.kind = 'store'
join variants v on v.id = m.variant_id
join products p on p.id = v.product_id
where (@locs::uuid[] is null or m.location_id = any(@locs::uuid[])) and ` + productFilter + `
  and coalesce((select sum(b.on_hand) from stock_balances b join zones z on z.id = b.zone_id
                where b.variant_id = m.variant_id and b.location_id = m.location_id and z.kind = 'floor'), 0) = 0
  and exists (select 1 from stock_balances b where b.variant_id = m.variant_id and b.location_id <> m.location_id and b.on_hand - b.reserved > 0)
order by l.code, p.name`,
	},
	{
		Code: "transfer_suggestions", Title: "Рекомендации по перемещению",
		Description: "Где не хватает до минимума матрицы и откуда взять: точка с наибольшим излишком",
		Params:      []string{"locations", "categories", "brands"},
		Roles:       []string{manager, storeManager, storekeeper},
		Columns: []Column{{"Куда", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false}, {"Не хватает", Int, true},
			{"Откуда", Text, false}, {"Излишек там", Int, false}, {"Переместить", Int, true}},
		SQL: catTree + `, need as (
	select m.location_id, m.variant_id, m.min_qty - ` + available + ` as qty
	from assortment_matrix m
	where (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))
), surplus as (
	select b.location_id, b.variant_id,
	       sum(b.on_hand - b.reserved) - coalesce((select am.min_qty from assortment_matrix am
	           where am.location_id = b.location_id and am.variant_id = b.variant_id), 0) as qty
	from stock_balances b group by b.location_id, b.variant_id
)
select l.code || ' · ' || l.name, p.name, v.article, n.qty::int, dl.code || ' · ' || dl.name, d.qty::int, least(n.qty, d.qty)::int
from need n
join lateral (select s.location_id, s.qty from surplus s where s.variant_id = n.variant_id and s.location_id <> n.location_id
              and s.qty > 0 order by s.qty desc limit 1) d on true
join locations l on l.id = n.location_id
join locations dl on dl.id = d.location_id
join variants v on v.id = n.variant_id
join products p on p.id = v.product_id
where n.qty > 0 and ` + productFilter + `
order by l.code, p.name`,
	},
	{
		Code: "replenishment", Title: "Товары к пополнению",
		Description: "Остаток ниже минимального по ассортиментной матрице",
		Params:      []string{"locations", "categories", "brands"},
		Roles:       []string{manager, storeManager, storekeeper},
		Columns: []Column{{"Точка", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false}, {"Минимум", Int, false},
			{"Свободный остаток", Int, false}, {"Заказать", Int, true}},
		SQL: catTree + `
select l.code || ' · ' || l.name, p.name, v.article, m.min_qty, ` + available + `::int, (m.min_qty - ` + available + `)::int
from assortment_matrix m
join locations l on l.id = m.location_id
join variants v on v.id = m.variant_id
join products p on p.id = v.product_id
where (@locs::uuid[] is null or m.location_id = any(@locs::uuid[])) and ` + productFilter + `
  and ` + available + ` < m.min_qty
order by l.code, p.name`,
	},
	{
		Code: "writeoffs", Title: "Бой и списания",
		Description: "Списания по причинам, точкам и сотрудникам за период",
		Params:      []string{"period", "locations"},
		Roles:       []string{manager, accountant, storeManager},
		Columns: []Column{{"Причина", Text, false}, {"Точка", Text, false}, {"Сотрудник", Text, false}, {"Документов", Int, true},
			{"Кол-во", Int, true}, {"Себестоимость, ₽", Money, true}},
		SQL: `
select coalesce(wr.name, 'Недостача перемещения'), l.code || ' · ' || l.name, coalesce(u.full_name, '—'),
       count(distinct d.id), coalesce(-sum(m.qty), 0)::int, round(coalesce(-sum(m.cost_amount), 0), 2)
from documents d
join locations l on l.id = d.location_id
left join writeoff_reasons wr on wr.id = d.reason_id
left join users u on u.id = d.author_id
left join stock_moves m on m.document_id = d.id
where d.kind = 'writeoff' and d.status in ('posted', 'awaiting_egais') and d.doc_date between @from::date and @to::date
  and (@locs::uuid[] is null or d.location_id = any(@locs::uuid[]))
group by 1, 2, 3
order by 6 desc`,
	},
	{
		Code: "deficits", Title: "Недостачи к разбору",
		Description: "Товар без марок, проданный сверх учётного остатка, и документы, которыми недостача закрыта",
		Params:      []string{"period", "locations"},
		Roles:       []string{manager, accountant, storeManager},
		Columns: []Column{{"Дата", Date, false}, {"Точка", Text, false}, {"Товар", Text, false}, {"Артикул", Text, false},
			{"Возникла, шт", Int, true}, {"Закрыта, шт", Int, true}, {"Документ", Text, false}},
		SQL: `
select m.doc_date, l.code || ' · ' || l.name, p.name, v.article, greatest(m.qty, 0), greatest(-m.qty, 0), d.number
from stock_moves m
join documents d on d.id = m.document_id
join locations l on l.id = m.location_id
join variants v on v.id = m.variant_id
join products p on p.id = v.product_id
where m.bucket = 'deficit' and m.doc_date between @from::date and @to::date and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))
order by m.doc_date desc, m.id desc`,
	},
	{
		Code: "reservations", Title: "Брони и заявки",
		Description: "Брони и заявки на перемещение за период по исходу: выполнено, просрочено, отменено",
		Params:      []string{"period", "locations"},
		Roles:       []string{manager, storeManager},
		Columns:     []Column{{"Вид", Text, false}, {"Точка", Text, false}, {"Исход", Text, false}, {"Количество", Int, true}},
		SQL: `
select 'Брони', l.code || ' · ' || l.name,
       case r.status when 'issued' then 'Выдана' when 'expired' then 'Просрочена' when 'cancelled' then 'Снята'
                     when 'confirmed' then 'Отложена, ждёт покупателя' else 'Ждёт подтверждения' end, count(*)
from reservations r join locations l on l.id = r.location_id
where (r.created_at at time zone l.timezone)::date between @from::date and @to::date and (@locs::uuid[] is null or r.location_id = any(@locs::uuid[]))
group by 2, 3
union all
select 'Заявки на перемещение', dl.code || ' · ' || dl.name,
       case d.status when 'requested' then 'Ждёт согласования' when 'rejected' then 'Отклонена' when 'draft' then 'Собирается'
                     when 'in_transit' then 'В пути' when 'cancelled' then 'Отменена' else 'Выполнена' end, count(*)
from documents d join locations dl on dl.id = d.dest_location_id
where d.kind = 'transfer' and d.doc_date between @from::date and @to::date and (@locs::uuid[] is null or d.dest_location_id = any(@locs::uuid[]))
group by 2, 3
order by 1, 2, 3`,
	},
}
