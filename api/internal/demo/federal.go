package demo

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Федеральная сеть для нагрузочных испытаний (решение 0063): тысячи магазинов по городам России
// пропорционально населению и региональные склады. Пишется только текущее состояние — остатки,
// марки, матрицы — с одним документом ввода остатков на зону, без истории: годовая история такой
// сети заняла бы сотни гигабайт. Загрузка идёт через COPY.

// city — «Город|код субъекта|часовой пояс|широта|долгота|население, тыс.».
const cities = `
Москва|77|Europe/Moscow|55.756|37.617|13100
Санкт-Петербург|78|Europe/Moscow|59.939|30.316|5600
Новосибирск|54|Asia/Novosibirsk|55.030|82.920|1630
Екатеринбург|66|Asia/Yekaterinburg|56.838|60.597|1540
Казань|16|Europe/Moscow|55.796|49.106|1310
Нижний Новгород|52|Europe/Moscow|56.327|44.006|1220
Красноярск|24|Asia/Krasnoyarsk|56.010|92.852|1200
Челябинск|74|Asia/Yekaterinburg|55.160|61.402|1180
Самара|63|Europe/Samara|53.195|50.101|1160
Уфа|02|Asia/Yekaterinburg|54.735|55.958|1160
Ростов-на-Дону|61|Europe/Moscow|47.222|39.720|1140
Краснодар|23|Europe/Moscow|45.035|38.975|1100
Омск|55|Asia/Omsk|54.989|73.368|1110
Воронеж|36|Europe/Moscow|51.672|39.184|1050
Пермь|59|Asia/Yekaterinburg|58.010|56.229|1030
Волгоград|34|Europe/Volgograd|48.708|44.513|1020
Саратов|64|Europe/Saratov|51.533|46.034|900
Тюмень|72|Asia/Yekaterinburg|57.153|65.534|850
Тольятти|63|Europe/Samara|53.508|49.420|680
Барнаул|22|Asia/Barnaul|53.348|83.779|630
Ижевск|18|Europe/Samara|56.852|53.204|630
Хабаровск|27|Asia/Vladivostok|48.480|135.072|610
Ульяновск|73|Europe/Ulyanovsk|54.314|48.403|610
Иркутск|38|Asia/Irkutsk|52.287|104.305|610
Владивосток|25|Asia/Vladivostok|43.116|131.886|600
Ярославль|76|Europe/Moscow|57.626|39.894|570
Севастополь|92|Europe/Simferopol|44.616|33.525|550
Ставрополь|26|Europe/Moscow|45.044|41.969|550
Томск|70|Asia/Tomsk|56.484|84.948|560
Кемерово|42|Asia/Novokuznetsk|55.354|86.088|550
Набережные Челны|16|Europe/Moscow|55.743|52.396|550
Оренбург|56|Asia/Yekaterinburg|51.768|55.097|550
Новокузнецк|42|Asia/Novokuznetsk|53.757|87.136|540
Рязань|62|Europe/Moscow|54.630|39.736|530
Балашиха|50|Europe/Moscow|55.796|37.938|520
Пенза|58|Europe/Moscow|53.195|45.018|500
Чебоксары|21|Europe/Moscow|56.146|47.251|490
Липецк|48|Europe/Moscow|52.610|39.594|500
Калининград|39|Europe/Kaliningrad|54.710|20.511|490
Астрахань|30|Europe/Astrakhan|46.348|48.033|470
Тула|71|Europe/Moscow|54.193|37.618|470
Киров|43|Europe/Kirov|58.604|49.668|470
Сочи|23|Europe/Moscow|43.585|39.723|440
Курск|46|Europe/Moscow|51.730|36.193|440
Улан-Удэ|03|Asia/Irkutsk|51.834|107.584|430
Тверь|69|Europe/Moscow|56.859|35.912|420
Магнитогорск|74|Asia/Yekaterinburg|53.407|58.980|410
Сургут|86|Asia/Yekaterinburg|61.254|73.396|400
Брянск|32|Europe/Moscow|53.243|34.364|380
Иваново|37|Europe/Moscow|57.000|40.973|360
Якутск|14|Asia/Yakutsk|62.028|129.732|360
Владимир|33|Europe/Moscow|56.129|40.407|350
Симферополь|91|Europe/Simferopol|44.952|34.102|340
Белгород|31|Europe/Moscow|50.596|36.587|340
Нижний Тагил|66|Asia/Yekaterinburg|57.908|59.973|340
Чита|75|Asia/Chita|52.034|113.500|350
Архангельск|29|Europe/Moscow|64.539|40.516|300
Калуга|40|Europe/Moscow|54.514|36.262|330
Смоленск|67|Europe/Moscow|54.783|32.045|310
Курган|45|Asia/Yekaterinburg|55.441|65.341|310
Орёл|57|Europe/Moscow|52.970|36.064|300
Саранск|13|Europe/Moscow|54.187|45.184|310
Мурманск|51|Europe/Moscow|68.970|33.075|270
Тамбов|68|Europe/Moscow|52.721|41.452|260
Петрозаводск|10|Europe/Moscow|61.786|34.346|280
Кострома|44|Europe/Moscow|57.768|40.927|270
Йошкар-Ола|12|Europe/Moscow|56.634|47.900|280
Новороссийск|23|Europe/Moscow|44.723|37.769|270
Сыктывкар|11|Europe/Moscow|61.668|50.836|250
Благовещенск|28|Asia/Yakutsk|50.290|127.527|240
Великий Новгород|53|Europe/Moscow|58.522|31.270|220
Псков|60|Europe/Moscow|57.819|28.332|210
Южно-Сахалинск|65|Asia/Sakhalin|46.959|142.738|200
Петропавловск-Камчатский|41|Asia/Kamchatka|53.024|158.643|180
`

// subjects — субъекты РФ, где стоят магазины федеральной сети: справочник дополняется ими.
const subjects = "77=Москва;78=Санкт-Петербург;54=Новосибирская область;66=Свердловская область;16=Республика Татарстан;52=Нижегородская область;24=Красноярский край;74=Челябинская область;63=Самарская область;02=Республика Башкортостан;61=Ростовская область;23=Краснодарский край;55=Омская область;36=Воронежская область;59=Пермский край;34=Волгоградская область;64=Саратовская область;72=Тюменская область;22=Алтайский край;18=Удмуртская Республика;27=Хабаровский край;73=Ульяновская область;38=Иркутская область;25=Приморский край;76=Ярославская область;92=Севастополь;26=Ставропольский край;70=Томская область;42=Кемеровская область — Кузбасс;56=Оренбургская область;62=Рязанская область;50=Московская область;58=Пензенская область;21=Чувашская Республика;48=Липецкая область;39=Калининградская область;30=Астраханская область;71=Тульская область;43=Кировская область;46=Курская область;03=Республика Бурятия;69=Тверская область;86=Ханты-Мансийский автономный округ — Югра;32=Брянская область;37=Ивановская область;14=Республика Саха (Якутия);33=Владимирская область;91=Республика Крым;31=Белгородская область;75=Забайкальский край;29=Архангельская область;40=Калужская область;67=Смоленская область;45=Курганская область;57=Орловская область;13=Республика Мордовия;51=Мурманская область;68=Тамбовская область;10=Республика Карелия;44=Костромская область;12=Республика Марий Эл;11=Республика Коми;28=Амурская область;53=Новгородская область;60=Псковская область;65=Сахалинская область;41=Камчатский край"

type cityRow struct {
	name, subject, tz string
	lat, lon, pop     float64
}

func parseCities() []cityRow {
	var out []cityRow
	for _, l := range strings.Split(strings.TrimSpace(cities), "\n") {
		f := strings.Split(l, "|")
		lat, _ := strconv.ParseFloat(f[3], 64)
		lon, _ := strconv.ParseFloat(f[4], 64)
		pop, _ := strconv.ParseFloat(f[5], 64)
		out = append(out, cityRow{f[0], f[1], f[2], lat, lon, pop})
	}
	return out
}

// FederalOptions — размер федеральной сети: Stores магазинов, из них доля SmallShare — у дома.
type FederalOptions struct {
	Stores     int
	SmallShare float64
}

var streets = []string{"Ленина", "Мира", "Советская", "Гагарина", "Молодёжная", "Школьная", "Садовая", "Лесная",
	"Пушкина", "Победы", "Кирова", "Строителей", "Набережная", "Октябрьская", "Комсомольская", "Заводская"}

// Federal добавляет федеральную сеть к каталогу из catalog-full. owner — подключение владельца
// схемы: на время загрузки отключаются триггеры событий реального времени.
func Federal(ctx context.Context, owner *pgxpool.Pool, opts FederalOptions, log *slog.Logger) error {
	g := &gen{pool: owner, rnd: mrand.New(mrand.NewPCG(2026, 3000)), log: log}
	if err := g.setup(ctx); err != nil {
		return err
	}
	list := parseCities()
	var total float64
	for _, c := range list {
		total += c.pop
	}
	began := time.Now()
	for _, kv := range strings.Split(subjects, ";") {
		code, name, _ := strings.Cut(kv, "=")
		if _, err := owner.Exec(ctx, `insert into ru_subjects (code, name) values ($1, $2) on conflict do nothing`, code, name); err != nil {
			return err
		}
	}
	if _, err := owner.Exec(ctx, `alter table stock_balances disable trigger stock_balances_events;
		alter table documents disable trigger documents_events`); err != nil {
		return err
	}
	defer func() {
		_, _ = owner.Exec(context.Background(), `alter table stock_balances enable trigger stock_balances_events;
			alter table documents enable trigger documents_events`)
	}()

	// Магазины — пропорционально населению, склад — на каждые сто магазинов города, но не меньше одного.
	var plan []federalRow
	n := 0
	for _, c := range list {
		for range max(1, int(math.Round(float64(opts.Stores)*c.pop/total))) {
			if n >= opts.Stores {
				break
			}
			n++
			plan = append(plan, federalRow{c, g.rnd.Float64() < opts.SmallShare, n})
		}
	}
	// Остаток от округления — в крупнейшие города, чтобы магазинов было ровно opts.Stores.
	for k := 0; n < opts.Stores; k++ {
		n++
		plan = append(plan, federalRow{list[k%len(list)], g.rnd.Float64() < opts.SmallShare, n})
	}
	// Точки «Чекушки» из dev.sql в федеральной сети не нужны: закрываются, чтобы не висеть пустыми.
	if _, err := owner.Exec(ctx, `update locations set is_active = false where code not like 'Ф%' and kind in ('store', 'warehouse')`); err != nil {
		return err
	}
	stores, whs, err := g.federalPoints(ctx, plan)
	if err != nil {
		return err
	}
	log.Info("federal network", "stores", len(stores), "warehouses", len(whs), "elapsed", time.Since(began).Round(time.Second))
	// Матрица магазина — компактный список; спрос считается заново при записи остатков. Склад держит
	// запас на два дня продаж своих магазинов.
	for _, s := range stores {
		g.assortment(ctx, []*point{s})
		for _, i := range s.assort {
			if !g.items[i].quantity {
				s.wh.demand[i] += s.demand[i] * 2
			}
		}
		s.demand, s.in = nil, nil
	}
	day := time.Now().AddDate(0, 0, -1)
	seq := 0
	batch := &federalBatch{}
	all := append(append([]*point{}, whs...), stores...)
	for k, p := range all {
		if p.kind == "store" {
			p.demand = g.demandOf(p)
		}
		docs := map[uuid.UUID]uuid.UUID{}
		zones := []uuid.UUID{p.zone}
		if p.kind == "store" {
			zones = append(zones, p.floor)
		}
		for _, z := range zones {
			id := uuid.Must(uuid.NewV7())
			docs[z] = id
			g.seq++
			batch.docs = append(batch.docs, []any{id, "opening", fmt.Sprintf("ВО-%d-Ф%07d", day.Year(), g.seq), "posted",
				pgtype.Date{Time: day, Valid: true}, p.id, z, g.author, g.author, day, day, "Ввод остатков федеральной сети"})
		}
		for _, i := range p.lines() {
			it := g.items[i]
			var back, floor int32
			if p.kind == "warehouse" {
				back = int32(math.Ceil(p.demand[i])) + 6
			} else {
				d := p.demand[i]
				total := int32(math.Ceil(d*7)) + 8
				floor = int32(max(4, math.Ceil(d*2)))
				if p.small {
					total = int32(math.Ceil(d*7)) + 4
					floor = int32(max(3, math.Ceil(d*2)))
				}
				back = max(total-floor, 0)
			}
			for _, zq := range []struct {
				zone uuid.UUID
				qty  int32
			}{{p.zone, back}, {p.floor, floor}} {
				if zq.qty == 0 {
					continue
				}
				cost := int64(math.Round(float64(it.costE4) * float64(zq.qty) / 100))
				batch.balances = append(batch.balances, []any{it.id, zq.zone, p.id, zq.qty})
				batch.moves = append(batch.moves, []any{docs[zq.zone], it.id, p.id, zq.zone, "on_hand", zq.qty,
					kop(cost), pgtype.Date{Time: day, Valid: true}, day})
				if it.marked {
					for range zq.qty {
						seq++
						batch.marks = append(batch.marks, []any{fmt.Sprintf("PKN%013s", strings.ToUpper(strconv.FormatInt(int64(seq), 36))),
							it.id, p.id, zq.zone, "in_stock", docs[zq.zone]})
					}
				}
			}
		}
		if p.kind == "store" {
			for _, i := range p.assort {
				batch.matrix = append(batch.matrix, []any{p.id, g.items[i].id, int32(max(2, math.Ceil(p.demand[i]*2)))})
			}
			p.demand = nil
		}
		if len(batch.marks) > 400_000 || k == len(all)-1 {
			if err := batch.copy(ctx, owner); err != nil {
				return err
			}
			batch = &federalBatch{}
			log.Info("federal stock", "points", k+1, "of", len(all), "marks", seq, "elapsed", time.Since(began).Round(time.Second))
		}
	}
	// Средняя себестоимость — закупочная цена каталога, запас организации — всё, что ввели.
	if _, err := owner.Exec(ctx, `insert into variant_costs (variant_id, qty_total, avg_cost)
		select v.id, coalesce((select sum(b.on_hand) from stock_balances b where b.variant_id = v.id), 0)::int,
		       coalesce((select pr.price from prices pr where pr.variant_id = v.id and pr.kind = 'purchase' order by pr.valid_from desc limit 1), 0)
		from variants v
		on conflict (variant_id) do update set qty_total = excluded.qty_total, avg_cost = excluded.avg_cost`); err != nil {
		return err
	}
	_, err = owner.Exec(ctx, `analyze`)
	log.Info("federal network ready", "marks", seq, "elapsed", time.Since(began).Round(time.Second))
	return err
}

// lines — что лежит на точке: у магазина — матрица, у склада — всё, что продают его магазины.
func (p *point) lines() []int {
	if p.kind == "store" {
		return p.assort
	}
	out := make([]int, 0, len(p.demand))
	for i := range p.demand {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// demandOf — продажи позиций матрицы в день, как в assortment.
func (g *gen) demandOf(s *point) map[int]float64 {
	var total float64
	for _, i := range s.assort {
		total += g.items[i].popularity
	}
	units := receiptsPerDay(s) * linesPerReceipt
	out := make(map[int]float64, len(s.assort))
	for _, i := range s.assort {
		out[i] = units * g.items[i].popularity / total
	}
	return out
}

type federalRow struct {
	city  cityRow
	small bool
	n     int
}

// federalPoints заводит магазины и склады: коды ФМ-0001 и ФС-001, ФСРАР ИД и лицензии вымышленные.
func (g *gen) federalPoints(ctx context.Context, plan []federalRow) ([]*point, []*point, error) {
	hours := `{"mon": {"open": "09:00", "close": "23:00"}, "tue": {"open": "09:00", "close": "23:00"},
		"wed": {"open": "09:00", "close": "23:00"}, "thu": {"open": "09:00", "close": "23:00"},
		"fri": {"open": "09:00", "close": "23:00"}, "sat": {"open": "10:00", "close": "23:00"}, "sun": {"open": "10:00", "close": "22:00"}}`
	byCity := map[string][]federalRow{}
	var order []string
	for _, p := range plan {
		if _, ok := byCity[p.city.name]; !ok {
			order = append(order, p.city.name)
		}
		byCity[p.city.name] = append(byCity[p.city.name], p)
	}
	var stores, whs []*point
	wn := 0
	err := pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		insert := func(code, name, kind string, c cityRow, lat, lon float64, n int) (*point, error) {
			tz, err := time.LoadLocation(c.tz)
			if err != nil {
				return nil, err
			}
			p := &point{code: code, kind: kind, tz: tz, lat: lat, lon: lon, in: map[int]bool{}, demand: map[int]float64{},
				traffic: 0.8 + 0.4*g.rnd.Float64()}
			err = tx.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code, address, lat, lon,
					fsrar_id, kpp, license_no, license_valid_to, opening_hours)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, current_date + 900, $13) returning id`,
				code, name, kind, c.name, c.tz, c.subject, fmt.Sprintf("ул. %s, %d", streets[n%len(streets)], 1+n%150),
				lat, lon, fmt.Sprintf("0400%08d", n), fmt.Sprintf("%s01%05d", c.subject, n%100000),
				fmt.Sprintf("%sРПО%07d", c.subject, n), hours).Scan(&p.id)
			if err != nil {
				return nil, err
			}
			return p, tx.QueryRow(ctx, `select (select id from zones where location_id = $1 and kind in ('backroom', 'warehouse') order by kind limit 1),
				coalesce((select id from zones where location_id = $1 and kind = 'floor'), '00000000-0000-0000-0000-000000000000')`, p.id).Scan(&p.zone, &p.floor)
		}
		for _, name := range order {
			rows := byCity[name]
			c := rows[0].city
			var cityWhs []*point
			for k := range max(1, (len(rows)+99)/100) {
				wn++
				w, err := insert(fmt.Sprintf("ФС-%03d", wn), fmt.Sprintf("Распределительный центр %s %d", c.name, k+1), "warehouse",
					c, c.lat+0.02*g.rnd.NormFloat64(), c.lon+0.03*g.rnd.NormFloat64(), 9000+wn)
				if err != nil {
					return err
				}
				cityWhs = append(cityWhs, w)
				whs = append(whs, w)
			}
			for k, r := range rows {
				s, err := insert(fmt.Sprintf("ФМ-%04d", r.n), fmt.Sprintf("Магазин %s №%d", c.name, k+1), "store",
					c, c.lat+0.04*g.rnd.NormFloat64(), c.lon+0.06*g.rnd.NormFloat64(), r.n)
				if err != nil {
					return err
				}
				s.small = r.small
				s.wh = cityWhs[k%len(cityWhs)]
				s.wh.stores = append(s.wh.stores, s)
				stores = append(stores, s)
			}
		}
		return nil
	})
	return stores, whs, err
}

type federalBatch struct {
	docs, balances, moves, marks, matrix [][]any
}

func (b *federalBatch) copy(ctx context.Context, pool *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, t := range []struct {
			table string
			cols  []string
			rows  [][]any
		}{
			{"documents", []string{"id", "kind", "number", "status", "doc_date", "location_id", "zone_id", "author_id", "posted_by", "posted_at", "created_at", "comment"}, b.docs},
			{"stock_balances", []string{"variant_id", "zone_id", "location_id", "on_hand"}, b.balances},
			{"stock_moves", []string{"document_id", "variant_id", "location_id", "zone_id", "bucket", "qty", "cost_amount", "doc_date", "posted_at"}, b.moves},
			{"excise_marks", []string{"code", "variant_id", "location_id", "zone_id", "status", "last_document_id"}, b.marks},
			{"assortment_matrix", []string{"location_id", "variant_id", "min_qty"}, b.matrix},
		} {
			if len(t.rows) == 0 {
				continue
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.table}, t.cols, pgx.CopyFromRows(t.rows)); err != nil {
				return fmt.Errorf("%s: %w", t.table, err)
			}
		}
		return nil
	})
}
