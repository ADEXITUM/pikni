package seed

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
)

func fixed(v float64) func(*rand.Rand) float64 { return func(*rand.Rand) float64 { return v } }

// between — крепость с шагом 0,5 %: у вина и пива она на этикетке именно такая.
func between(lo, hi float64) func(*rand.Rand) float64 {
	return func(r *rand.Rand) float64 {
		steps := int((hi - lo) / 0.5)
		return lo + 0.5*float64(r.IntN(steps+1))
	}
}

func pick(r *rand.Rand, xs ...string) string { return xs[r.IntN(len(xs))] }

func glass(l float64) pack { return pack{ml: int(l*1000 + 0.5), container: "glass"} }
func can(l float64) pack   { return pack{ml: int(l*1000 + 0.5), container: "can"} }
func pet(l float64) pack   { return pack{ml: int(l*1000 + 0.5), container: "pet"} }
func gift(l float64) pack  { return pack{ml: int(l*1000 + 0.5), container: "glass", gift: true} }

func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" && !strings.HasSuffix(p, ": ") {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}

var yearsRe = regexp.MustCompile(`(\d+) (?:лет|года|год)`)

// aging — выдержка из названия линейки: «12 лет», «5 звёзд», VSOP.
func aging(l *lineRow) string {
	t := l.text
	if m := yearsRe.FindStringSubmatch(t); m != nil {
		return "Выдержка: " + m[1]
	}
	for _, x := range []struct{ s, y string }{{"XO", "6"}, {"VSOP", "4"}, {"VS", "2"}, {"5 звёзд", "5"}, {"3 звезды", "3"}, {"7*", "7"}, {"5*", "5"}} {
		if strings.Contains(t, x.s) {
			return "Выдержка: " + x.y
		}
	}
	return ""
}

func flavor(b *brandRow, l *lineRow) string {
	if f := l.opt(b, "f"); f != "" {
		return "Вкус: " + f
	}
	return ""
}

var beerStyles = []string{"светлое", "тёмное", "пшеничное", "эль", "IPA", "стаут", "портер"}

var spirits = []extraPack{{glass(0.1), 0.5}, {glass(0.25), 0.75}, {glass(0.375), 0.3}, {glass(0.7), 0.55}, {glass(1), 0.5}, {gift(0.5), 0.1}}
var imported = []extraPack{{glass(0.5), 0.45}, {glass(1), 0.35}, {glass(0.2), 0.15}, {glass(0.05), 0.1}, {gift(0.7), 0.25}}

var kinds = map[string]*kind{
	"vodka": {category: "Крепкий алкоголь / Водка", noun: "Водка", egais: "200", ref: 500,
		main: []pack{glass(0.5)}, extra: spirits, abv: fixed(40),
		attrs: func(b *brandRow, l *lineRow, r *rand.Rand) string {
			raw := l.opt(b, "r")
			if raw == "" {
				raw = pick(r, "пшеница", "пшеница", "смесь зерновых", "рожь")
			}
			return join("Сырьё: "+raw, "Фильтрация: "+pick(r, "угольная", "серебряная", "берёзовая", "многоступенчатая"))
		}},
	"cognac": {category: "Крепкий алкоголь / Коньяк и бренди", noun: "Коньяк", egais: "2291", ref: 500,
		main: []pack{glass(0.5)}, extra: []extraPack{{glass(0.25), 0.7}, {glass(0.1), 0.4}, {glass(0.7), 0.5}, {gift(0.5), 0.35}},
		abv: fixed(40), attrs: func(_ *brandRow, l *lineRow, _ *rand.Rand) string { return aging(l) }},
	"whisky": {category: "Крепкий алкоголь / Виски", noun: "Виски", egais: "237", ref: 700,
		main: []pack{glass(0.7)}, extra: imported, abv: fixed(40),
		attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string {
			t := l.opt(b, "t")
			if t != "" {
				t = "Тип: " + t
			}
			return join(aging(l), t)
		}},
	"rum": {category: "Крепкий алкоголь / Ром", noun: "Ром", egais: "236", ref: 700,
		main: []pack{glass(0.7)}, extra: imported, abv: fixed(40), attrs: noAttrs},
	"gin": {category: "Крепкий алкоголь / Джин", noun: "Джин", egais: "2807", ref: 700,
		main: []pack{glass(0.7)}, extra: imported, abv: fixed(40), attrs: noAttrs},
	"tequila": {category: "Крепкий алкоголь / Текила", noun: "Текила", egais: "280", ref: 700,
		main: []pack{glass(0.7)}, extra: imported, abv: fixed(38), attrs: noAttrs},
	"liqueur": {category: "Крепкий алкоголь / Ликёры и настойки", noun: "Ликёр", egais: "21212", ref: 700,
		main: []pack{glass(0.7)}, extra: []extraPack{{glass(0.5), 0.4}, {glass(1), 0.2}, {glass(0.2), 0.15}},
		abv: fixed(20), attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string { return flavor(b, l) }},
	"still": {category: "Вино / Тихое", noun: "Вино", egais: "401", ref: 750,
		main: []pack{glass(0.75)}, extra: []extraPack{{glass(0.375), 0.06}, {glass(1.5), 0.04}, {glass(0.187), 0.05}},
		abv: between(11.5, 14), attrs: wineAttrs},
	"sparkling": {category: "Вино / Игристое", noun: "Вино игристое", egais: "440", ref: 750,
		main: []pack{glass(0.75)}, extra: []extraPack{{glass(0.2), 0.15}, {glass(0.375), 0.1}, {glass(1.5), 0.05}},
		abv: between(10.5, 12.5),
		attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string {
			c := l.opt(b, "c")
			if c == "" {
				c = "б"
			}
			return join("Цвет: "+colors[c], "Содержание сахара: "+sugar(b, l, "брют"))
		}},
	"fortified": {category: "Вино / Креплёное", noun: "Вино ликёрное", egais: "4114", ref: 750,
		main: []pack{glass(0.75)}, extra: []extraPack{{glass(0.5), 0.3}, {glass(1), 0.3}},
		abv: between(15, 19),
		attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string {
			return join("Цвет: "+colors[l.opt(b, "c")], "Содержание сахара: "+sugar(b, l, "сладкое"))
		}},
	"beer": {category: "Пиво и сидр / Пиво", noun: "Пиво", egais: "500", crpt: true, ref: 500,
		main: []pack{can(0.45)}, extra: []extraPack{{glass(0.5), 0.7}, {can(0.5), 0.45}, {pet(1.3), 0.55}, {glass(0.33), 0.25}, {pet(1.5), 0.25}},
		abv: between(4, 5.5),
		attrs: func(b *brandRow, l *lineRow, r *rand.Rand) string {
			style := l.opt(b, "st")
			low := strings.ToLower(l.text)
			for _, s := range beerStyles {
				if style == "" && strings.Contains(low, strings.ToLower(s)) {
					style = s
				}
			}
			if style == "" {
				style = "светлое"
			}
			filt := "фильтрованное"
			if strings.Contains(low, "нефильтр") || style == "пшеничное" {
				filt = "нефильтрованное"
			}
			return join("Стиль: "+style, "Фильтрация: "+filt)
		}},
	"cider": {category: "Пиво и сидр / Сидр и пуаре", noun: "Сидр", egais: "261", crpt: true, ref: 450,
		main: []pack{can(0.45)}, extra: []extraPack{{glass(0.33), 0.4}, {pet(1), 0.25}},
		abv: between(4.5, 6.5), attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string { return flavor(b, l) }},
	"rtd": {category: "Слабоалкогольные напитки", noun: "Напиток слабоалкогольный", egais: "2601", ref: 450,
		main: []pack{can(0.45)}, extra: []extraPack{{can(0.33), 0.4}},
		abv: between(5, 8), attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string { return flavor(b, l) }},
	"soft": {category: "Безалкогольное и снеки / Напитки", noun: "Напиток", crpt: true, ref: 500,
		main: []pack{pet(0.5)}, extra: []extraPack{{pet(1), 0.35}, {pet(1.5), 0.4}, {can(0.33), 0.3}},
		abv: fixed(0), attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string { return flavor(b, l) }},
	"snack": {category: "Безалкогольное и снеки / Снеки", noun: "Чипсы", ref: 0,
		abv: fixed(0), attrs: func(b *brandRow, l *lineRow, _ *rand.Rand) string { return flavor(b, l) }},
	"tobacco": {category: "Табачная продукция / Сигареты", noun: "Сигареты", crpt: true, ref: 0,
		main: []pack{{}}, abv: fixed(0), attrs: noAttrs},
}

func noAttrs(*brandRow, *lineRow, *rand.Rand) string { return "" }

func wineAttrs(b *brandRow, l *lineRow, r *rand.Rand) string {
	grape := l.opt(b, "gr")
	if grape == "" && isGrape(l.text) {
		grape = strings.ToLower(l.text)
	}
	if grape != "" {
		grape = "Сорт винограда: " + grape
	}
	var year string
	if r.IntN(10) < 4 {
		year = fmt.Sprintf("Год урожая: %d", 2021+r.IntN(4))
	}
	return join("Цвет: "+colors[l.opt(b, "c")], "Содержание сахара: "+sugar(b, l, "сухое"), grape, year)
}

var grapes = []string{"каберне", "мерло", "шардоне", "совиньон", "пино", "саперави", "ркацители", "рислинг",
	"мальбек", "карменер", "шираз", "темпранильо", "гарнача", "примитиво", "неро д'авола", "санджовезе",
	"монтепульчано", "мускат", "москато", "алиготе", "красностоп", "цимлянский", "пинотаж", "шенен",
	"торронтес", "вердехо", "гевюрцтраминер", "зинфандель", "грюнер", "бобаль", "сира", "треббьяно", "арени",
	"горули", "гарганега", "кортезе", "барбера", "неграмаро", "фетяска", "кокур", "сибирьковый"}

func isGrape(s string) bool {
	low := strings.ToLower(s)
	for _, g := range grapes {
		if strings.HasPrefix(low, g) {
			return true
		}
	}
	return false
}
