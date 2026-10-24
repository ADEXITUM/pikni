package sim

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Участники совпадают с демо-данными «Пикни» (api/db/seed/dev.sql): точки сети и поставщики.
var demoParticipants = []Participant{
	{FSRARID: "030000100001", INN: "2801000010", KPP: "280101001", Name: "Чекушка, Центральный склад", Kind: "warehouse", Address: "Благовещенск, ул. Мухина, 150"},
	{FSRARID: "030000100002", INN: "2801000010", KPP: "272401001", Name: "Чекушка, Региональный склад", Kind: "warehouse", Address: "Хабаровск, ул. Промышленная, 21"},
	{FSRARID: "030000100011", INN: "2801000010", KPP: "280101002", Name: "Чекушка, магазин на ул. Ленина", Kind: "retail_point", Address: "Благовещенск, ул. Ленина, 112"},
	{FSRARID: "030000100012", INN: "2801000010", KPP: "280101003", Name: "Чекушка, магазин на ул. 50 лет Октября", Kind: "retail_point", Address: "Благовещенск, ул. 50 лет Октября, 65"},
	{FSRARID: "030000100013", INN: "2801000010", KPP: "272401002", Name: "Чекушка, магазин на ул. Муравьёва-Амурского", Kind: "retail_point", Address: "Хабаровск, ул. Муравьёва-Амурского, 18"},
	{FSRARID: "030000100014", INN: "2801000010", KPP: "254001001", Name: "Чекушка, магазин на ул. Светланской", Kind: "retail_point", Address: "Владивосток, ул. Светланская, 33"},
	{FSRARID: "030000200001", INN: "2801001056", KPP: "280101004", Name: "ООО «Амур-Дистрибуция»", Kind: "supplier"},
	{FSRARID: "030000200002", INN: "2724002077", KPP: "272401003", Name: "ООО «Хабаровский винный дом»", Kind: "supplier"},
	{FSRARID: "030000200003", INN: "2540003095", KPP: "254001002", Name: "ООО «Приморская пивная компания»", Kind: "supplier"},
}

// Виды продукции, которые учитываются в ЕГАИС количеством, без марок: пиво, сидр, пуаре, медовуха.
var quantityKinds = map[string]bool{"500": true, "510": true, "520": true, "261": true, "262": true, "263": true}

func number(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
	return v, err == nil
}

func optional(s string) *string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return &s
}

// Seed заводит участников и справочник продукции из демо-каталога «Пикни» (catalog.csv), чтобы
// алкокоды и штрихкоды поставок совпадали с каталогом. Повторный запуск обновляет записи.
func (s *Sim) Seed(ctx context.Context, catalog io.Reader) (int, error) {
	r := csv.NewReader(catalog)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("catalog is empty")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimPrefix(strings.TrimSpace(h), string(rune(0xFEFF)))] = i
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	n := 0
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, p := range demoParticipants {
			if _, err := tx.Exec(ctx, `insert into participants (fsrar_id, inn, kpp, name, kind, address)
				values ($1, $2, $3, $4, $5, $6) on conflict (fsrar_id) do update set inn = excluded.inn,
				kpp = excluded.kpp, name = excluded.name, kind = excluded.kind, address = excluded.address`,
				p.FSRARID, p.INN, p.KPP, p.Name, p.Kind, p.Address); err != nil {
				return err
			}
		}
		for _, row := range rows[1:] {
			alcocode := get(row, "Алкокод")
			if len(alcocode) != 19 {
				continue
			}
			kind := get(row, "Вид продукции")
			accounting := "marked"
			if quantityKinds[kind] {
				accounting = "quantity"
			}
			var volume *int
			if v, ok := number(get(row, "Объём, л")); ok {
				ml := int(v*1000 + 0.5)
				volume = &ml
			}
			var abv *float64
			if v, ok := number(get(row, "Крепость, %")); ok {
				abv = &v
			}
			price, _ := number(get(row, "Закупочная цена, ₽"))
			if _, err := tx.Exec(ctx, `insert into products (alcocode, name, kind_code, volume_ml, abv, producer,
				egais_accounting, barcode, gtin, crpt, price) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				on conflict (alcocode) do update set name = excluded.name, kind_code = excluded.kind_code,
				volume_ml = excluded.volume_ml, abv = excluded.abv, producer = excluded.producer,
				egais_accounting = excluded.egais_accounting, barcode = excluded.barcode, gtin = excluded.gtin,
				crpt = excluded.crpt, price = excluded.price`,
				alcocode, get(row, "Наименование"), kind, volume, abv, get(row, "Производитель"), accounting,
				optional(get(row, "Штрихкод")), optional(get(row, "GTIN")), get(row, "Честный знак") == "да", price); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
