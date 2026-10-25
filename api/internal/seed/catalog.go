// Package seed — данные для разработки и демонстрации: каталог реального ассортимента
// алкомаркета, сети разного размера и сценарии (решение 0063).
package seed

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Item — строка каталога: один вариант (объём, тара, подарочная упаковка) одной позиции.
type Item struct {
	Name, Category, Kind  string
	ABV                   float64
	Crpt                  bool
	Producer, Brand, Line string
	Variety               string
	Country, Region       string
	Attributes            string
	VolumeMl, WeightG     int
	Container             string
	Gift                  bool
	Alcocode, Barcode     string
	GTIN                  string
	Price, Purchase       int
}

type pack struct {
	ml, g     int
	container string
	gift      bool
}

// kind — правила раздела ассортимента: категория, начало наименования, код вида продукции,
// объём, к которому относится цена бренда, и обычный набор вариантов.
type kind struct {
	category, noun, egais string
	crpt                  bool
	ref                   int
	main                  []pack
	extra                 []extraPack
	abv                   func(r *rand.Rand) float64
	attrs                 func(b *brandRow, l *lineRow, r *rand.Rand) string
}

type extraPack struct {
	p    pack
	prob float64
}

type brandRow struct {
	kind                    *kind
	name, producer, country string
	region                  string
	price                   int
	opts                    map[string]string
	lines                   []lineRow
}

type lineRow struct {
	text string
	k    float64
	opts map[string]string
}

func (l *lineRow) opt(b *brandRow, key string) string {
	if v, ok := l.opts[key]; ok {
		return v
	}
	return b.opts[key]
}

// Catalog разворачивает описание ассортимента в строки каталога. Результат детерминирован:
// один и тот же каталог на любом компьютере.
func Catalog() ([]Item, error) {
	brands, err := parse(assortment)
	if err != nil {
		return nil, err
	}
	var out []Item
	codes := map[string]bool{}
	for _, b := range brands {
		for li := range b.lines {
			l := &b.lines[li]
			r := rand.New(rand.NewPCG(hash(b.name), hash(l.text)))
			abv := b.kind.abv(r)
			if v := l.opt(b, "abv"); v != "" {
				abv, _ = strconv.ParseFloat(v, 64)
			}
			egais := b.kind.egais
			if v := l.opt(b, "kind"); v != "" {
				egais = v
			}
			if egais == "500" && abv > 8.6 {
				egais = "510"
			}
			noun := b.kind.noun
			if v := l.opt(b, "n"); v != "" {
				noun = v
			}
			name, variety := title(noun, b, l)
			attrs := b.kind.attrs(b, l, r)
			for _, p := range packs(b, l, r) {
				it := Item{
					Name: name, Category: b.kind.category, Kind: egais, ABV: abv, Crpt: b.kind.crpt,
					Producer: b.producer, Brand: b.name, Variety: variety, Country: b.country, Region: b.region,
					Attributes: attrs, VolumeMl: p.ml, WeightG: p.g, Container: p.container, Gift: p.gift,
				}
				it.Price = max(price(float64(b.price)*l.k, b.kind.ref, p), minPrice(egais, abv, p.ml))
				it.Purchase = int(math.Round(float64(it.Price) * (0.58 + r.Float64()*0.12)))
				if b.kind.egais != "" {
					it.Alcocode = fmt.Sprintf("035%016d", len(out)+1)
				}
				it.Barcode = ean(b.country, name+fmt.Sprint(p), codes)
				if it.Crpt {
					it.GTIN = "0" + it.Barcode
				}
				out = append(out, it)
			}
		}
	}
	return out, nil
}

// price — цена варианта от цены бренда за базовый объём: маленькая бутылка дороже за литр.
// Округление — как на ценниках: 499, 1 290.
func price(base float64, ref int, p pack) int {
	x := base
	if p.ml > 0 && ref > 0 {
		x = base * math.Pow(float64(p.ml)/float64(ref), 0.88)
	}
	if p.gift {
		x *= 1.12
	}
	if x < 1000 {
		return int(math.Ceil(x/10))*10 - 1
	}
	return int(math.Ceil(x/100))*100 - 10
}

// minRetail — МРЦ за 0,5 л при крепости до 40 % (приказ Минфина № 181н, с 01.01.2026); крепче —
// на 3 % за каждый процент, с запасом к таблице приказа. Её же проверяет импорт каталога.
var minRetail = map[string]float64{"vodka": 409, "spirits": 409, "cognac": 755, "brandy": 605, "sparkling": 159.4}

var minGroup = map[string]string{
	"200": "vodka", "2002": "vodka",
	"212": "spirits", "2114": "spirits", "2123": "spirits", "2125": "spirits", "2127": "spirits", "21212": "spirits", "280": "spirits", "2807": "spirits",
	"229": "cognac", "2291": "cognac", "233": "cognac", "234": "cognac", "235": "cognac", "237": "cognac",
	"232": "brandy", "236": "brandy", "252": "brandy", "28011": "brandy", "2371": "brandy",
	"440": "sparkling", "441": "sparkling", "443": "sparkling", "450": "sparkling", "4501": "sparkling",
}

func minPrice(egais string, abv float64, ml int) int {
	base, ok := minRetail[minGroup[egais]]
	if !ok || ml == 0 {
		return 0
	}
	if abv > 40 {
		base *= 1 + 0.03*(abv-40)
	}
	return price(base*float64(ml)/500, 0, pack{})
}

func packs(b *brandRow, l *lineRow, r *rand.Rand) []pack {
	if v := l.opt(b, "v"); v != "" {
		return parsePacks(v)
	}
	if v := l.opt(b, "g"); v != "" {
		var out []pack
		for _, s := range strings.Split(v, ",") {
			g, _ := strconv.Atoi(s)
			out = append(out, pack{g: g})
		}
		return out
	}
	out := slices.Clone(b.kind.main)
	for _, e := range b.kind.extra {
		if r.Float64() < e.prob {
			out = append(out, e.p)
		}
	}
	return out
}

// parsePacks разбирает «0.33c,0.5g,1.3p,0.7gx»: объём в литрах, тара (c — банка, g — стекло,
// p — ПЭТ), x — подарочная упаковка.
func parsePacks(s string) []pack {
	var out []pack
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		gift := strings.HasSuffix(f, "x")
		f = strings.TrimSuffix(f, "x")
		c := map[byte]string{'c': "can", 'g': "glass", 'p': "pet"}[f[len(f)-1]]
		v, _ := strconv.ParseFloat(f[:len(f)-1], 64)
		out = append(out, pack{ml: int(math.Round(v * 1000)), container: c, gift: gift})
	}
	return out
}

func title(noun string, b *brandRow, l *lineRow) (string, string) {
	variety := l.text
	name := fmt.Sprintf("%s «%s»", noun, b.name)
	if variety != "" {
		name += " " + variety
	}
	// Цвет и сахар — в конце наименования, как на ценнике, если их нет в названии линейки.
	add := func(word string) {
		if !strings.Contains(strings.ToLower(name), word) {
			name += " " + word
		}
	}
	switch b.kind.category {
	case "Вино / Тихое":
		add(colors[l.opt(b, "c")])
		add(sugar(b, l, "сухое"))
	case "Вино / Игристое":
		if l.opt(b, "c") == "р" {
			add("розовое")
		}
		add(sugar(b, l, "брют"))
	}
	return strings.Join(strings.Fields(name), " "), strings.ToLower(variety)
}

var colors = map[string]string{"": "красное", "к": "красное", "б": "белое", "р": "розовое", "о": "оранжевое"}

func sugar(b *brandRow, l *lineRow, def string) string {
	if s := l.opt(b, "s"); s != "" {
		return s
	}
	return def
}

// ean — штрихкод EAN-13 с префиксом страны производителя и верной контрольной цифрой.
// Номера вымышленные; повтор внутри каталога исключён.
func ean(country, key string, used map[string]bool) string {
	prefix := eanPrefix[country]
	if prefix == "" {
		prefix = "460"
	}
	n := hash(key) % 1_000_000_000
	for {
		body := fmt.Sprintf("%s%09d", prefix, n)
		code := body + strconv.Itoa(checkDigit(body))
		if !used[code] {
			used[code] = true
			return code
		}
		n = (n + 1) % 1_000_000_000
	}
}

func checkDigit(body string) int {
	sum := 0
	for i, c := range body {
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return (10 - sum%10) % 10
}

var eanPrefix = map[string]string{
	"643": "460", "250": "330", "380": "800", "724": "841", "620": "560", "276": "400", "268": "486",
	"051": "485", "498": "484", "152": "780", "032": "779", "710": "600", "554": "942", "036": "930",
	"840": "008", "826": "501", "372": "539", "484": "750", "192": "850", "388": "501", "528": "871",
	"056": "541", "203": "859", "392": "490", "300": "520", "348": "599", "112": "481", "214": "746",
	"752": "730", "246": "641", "616": "590", "428": "475", "052": "501", "630": "008", "780": "501",
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// parse читает описание ассортимента: «@раздел» выбирает правила, строка бренда —
// «Бренд | Производитель | Страна | Регион | Цена | линейки через ; | {параметры бренда}».
// Линейка — «Название *множитель_цены {ключ=значение ...}».
func parse(text string) ([]*brandRow, error) {
	var out []*brandRow
	var cur *kind
	sc := bufio.NewScanner(strings.NewReader(text))
	n := 0
	for sc.Scan() {
		n++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "@") {
			cur = kinds[strings.TrimPrefix(s, "@")]
			if cur == nil {
				return nil, fmt.Errorf("line %d: unknown section %s", n, s)
			}
			continue
		}
		f := strings.Split(s, "|")
		if len(f) < 6 || cur == nil {
			return nil, fmt.Errorf("line %d: want 6 fields after a section: %q", n, s)
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		p, err := strconv.Atoi(f[4])
		if err != nil {
			return nil, fmt.Errorf("line %d: price %q", n, f[4])
		}
		b := &brandRow{kind: cur, name: f[0], producer: f[1], country: f[2], region: f[3], price: p, opts: map[string]string{}}
		if len(f) > 6 {
			b.opts = opts(f[6])
		}
		for _, ls := range strings.Split(f[5], ";") {
			l := lineRow{k: 1, opts: map[string]string{}}
			ls = strings.TrimSpace(ls)
			if i := strings.Index(ls, "{"); i >= 0 {
				l.opts = opts(ls[i:])
				ls = strings.TrimSpace(ls[:i])
			}
			if m := factorRe.FindStringSubmatch(ls); m != nil {
				l.k, _ = strconv.ParseFloat(m[1], 64)
				ls = strings.TrimSpace(ls[:len(ls)-len(m[0])])
			}
			if ls == "-" {
				ls = ""
			}
			l.text = ls
			b.lines = append(b.lines, l)
		}
		out = append(out, b)
	}
	return out, sc.Err()
}

// factorRe — множитель цены линейки « *1.35»; «5*» у бренди — часть названия.
var factorRe = regexp.MustCompile(` \*([0-9]+(?:\.[0-9]+)?)$`)

func opts(s string) map[string]string {
	m := map[string]string{}
	s = strings.Trim(strings.TrimSpace(s), "{}")
	for _, kv := range strings.Fields(s) {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = strings.ReplaceAll(v, "_", " ")
	}
	return m
}

// WriteCSV пишет каталог в формате импорта (колонки шаблона «Скачать шаблон»).
func WriteCSV(w io.Writer, items []Item) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	if err := cw.Write([]string{"Наименование", "Категория", "Вид продукции", "Крепость, %", "Честный знак",
		"Производитель", "Бренд", "Линейка", "Разновидность", "Страна", "Регион происхождения", "Характеристики",
		"Объём, л", "Тара", "Подарочная упаковка", "Алкокод", "Штрихкод", "Цена, ₽", "Закупочная цена, ₽",
		"Масса нетто, г", "GTIN"}); err != nil {
		return err
	}
	yes := map[bool]string{true: "да", false: "нет"}
	dec := func(f float64) string { return strings.Replace(strconv.FormatFloat(f, 'f', -1, 64), ".", ",", 1) }
	for _, it := range items {
		var abv, vol, weight string
		if it.Kind != "" {
			abv = dec(it.ABV)
		}
		if it.VolumeMl > 0 {
			vol = dec(float64(it.VolumeMl) / 1000)
		}
		if it.WeightG > 0 {
			weight = strconv.Itoa(it.WeightG)
		}
		container := map[string]string{"glass": "стекло", "pet": "ПЭТ", "can": "банка"}[it.Container]
		if err := cw.Write([]string{it.Name, it.Category, it.Kind, abv, yes[it.Crpt], it.Producer, it.Brand, it.Line,
			it.Variety, it.Country, it.Region, it.Attributes, vol, container, yes[it.Gift], it.Alcocode, it.Barcode,
			strconv.Itoa(it.Price), strconv.Itoa(it.Purchase), weight, it.GTIN}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
