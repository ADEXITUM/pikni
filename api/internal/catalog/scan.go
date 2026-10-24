package catalog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

// Scan — код, распознанный в поисковой строке. Code — штрихкод, алкокод или GTIN;
// у марки нового образца — её тип, серия и номер.
type Scan struct {
	Kind apispec.ScanKind
	Code string
	// Mark — текст марки ЕГАИС латиницей: по нему марка учитывается поштучно.
	Mark string
	// Unit — код «Честного знака» без кода проверки (GTIN и серийный номер): одна единица товара.
	Unit string
}

// ParseScan распознаёт считанный код для модулей, которые принимают сканы в документы.
func ParseScan(raw string) (Scan, bool) {
	return parseScan(raw)
}

// SearchFilter — поисковая строка, разобранная как в каталоге: часть артикула, слова
// наименования или код варианта. Nothing — искать нечего (марка нового образца без алкокода).
type SearchFilter = codeSearch

func ParseSearch(q string) SearchFilter {
	return parseSearch(q)
}

// crptSerial отделяет серийный номер кода «Честного знака» от кода проверки. Обычно после номера
// идёт разделитель GS; если сканер его потерял, длину подсказывает товарная группа: пиво — 7, вода — 13.
func crptSerial(rest string) string {
	if i := strings.IndexByte(rest, 0x1d); i >= 0 {
		return rest[:i]
	}
	for _, n := range []int{7, 13} {
		if len(rest) > n+2 && rest[n] == '9' && strings.ContainsRune("123", rune(rest[n+1])) {
			return rest[:n]
		}
	}
	return rest
}

var (
	markPattern  = regexp.MustCompile(`^[0-9A-Z]+$`)
	trainingMark = regexp.MustCompile(`^PKN[0-9A-Z]{10,}$`)
	wordPattern  = regexp.MustCompile(`[\p{L}\p{N}]+`)

	// Артикул присваивает база: «ЧК-000001». Его набирают вручную — бывает, без ведущих нулей
	// и в латинской раскладке, где на тех же клавишах «XR».
	articlePattern = regexp.MustCompile(`^(?i:ЧК|XR)-?([0-9]{1,6})$`)
	likeEscaper    = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

	// Сканер в русской раскладке печатает вместо латиницы буквы с тех же клавиш: «Й» вместо «Q».
	layout = func() *strings.Replacer {
		const ru, en = "ЙЦУКЕНГШЩЗФЫВАПРОЛДЯЧСМИТЬйцукенгшщзфывапролдячсмить", "QWERTYUIOPASDFGHJKLZXCVBNMqwertyuiopasdfghjklzxcvbnm"
		cyr, lat := []rune(ru), []rune(en)
		pairs := make([]string, 0, 2*len(cyr))
		for i := range cyr {
			pairs = append(pairs, string(cyr[i]), string(lat[i]))
		}
		return strings.NewReplacer(pairs...)
	}()
)

// parseScan распознаёт в строке скан марки ЕГАИС, код «Честного знака», алкокод или штрихкод.
func parseScan(raw string) (Scan, bool) {
	s := strings.TrimSpace(raw)
	// Некоторые сканеры добавляют префикс символики (]d2 у DataMatrix) и служебные FNC1 или GS.
	if len(s) > 3 && s[0] == ']' {
		s = s[3:]
	}
	s = strings.TrimLeft(s, "è\x1d")

	if m := articlePattern.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return Scan{Kind: apispec.ScanKindArticle, Code: fmt.Sprintf("ЧК-%06d", n)}, true
	}

	latin := strings.ToUpper(layout.Replace(s))
	if markPattern.MatchString(latin) {
		switch len(latin) {
		case 68:
			// Марка старого образца: в символах 8–19 — алкокод в системе счисления по основанию 36.
			if n, err := strconv.ParseUint(latin[7:19], 36, 64); err == nil {
				return Scan{Kind: apispec.ScanKindEgaisMark, Code: fmt.Sprintf("%019d", n), Mark: latin}, true
			}
		case 150:
			return Scan{Kind: apispec.ScanKindEgaisMarkNew, Code: latin[:14], Mark: latin}, true
		}
		// Учебные марки имитатора ЕГАИС начинаются с PKN и с настоящими не совпадают.
		// Алкокода в них нет, как и в марках нового образца.
		if trainingMark.MatchString(latin) {
			return Scan{Kind: apispec.ScanKindEgaisMarkNew, Code: latin, Mark: latin}, true
		}
	}

	// Код «Честного знака» начинается с (01) и 14 цифр GTIN, за ними (21) — серийный номер.
	if len(s) >= 18 && strings.HasPrefix(s, "01") && s[16:18] == "21" && gs1Valid(s[2:16]) {
		return Scan{Kind: apispec.ScanKindCrptCode, Code: s[2:16], Unit: s[2:16] + crptSerial(s[18:])}, true
	}
	// Код сигаретной пачки — 29 символов без разделителей: GTIN (14 цифр), серийный номер (7),
	// максимальная розничная цена в закодированном виде (4) и код проверки (4).
	if len(s) == 29 && digitsOnly.MatchString(s[:14]) && gs1Valid(s[:14]) &&
		!strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return Scan{Kind: apispec.ScanKindCrptCode, Code: s[:14], Unit: s[:21]}, true
	}
	if alcocodePattern.MatchString(s) {
		return Scan{Kind: apispec.ScanKindAlcocode, Code: s}, true
	}
	if digitsOnly.MatchString(s) && len(s) >= 8 {
		return Scan{Kind: apispec.ScanKindBarcode, Code: s}, true
	}
	// Code 128 на коробах и своих этикетках: латиница вместе с цифрами, без пробелов.
	if len(s) >= 4 && !digitsOnly.MatchString(s) && strings.ContainsAny(s, "0123456789") &&
		!strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return Scan{Kind: apispec.ScanKindBarcode, Code: s}, true
	}
	return Scan{}, false
}

// tsquery собирает запрос «все слова, каждое с начала»: «тайг пшен» → «тайг:* & пшен:*».
// Слова из одних цифр пропускаются: объём и крепость в наименовании обычно не пишут.
func tsquery(q string) *string {
	var terms []string
	for _, w := range wordPattern.FindAllString(q, -1) {
		if strings.IndexFunc(w, unicode.IsLetter) >= 0 {
			terms = append(terms, w+":*")
		}
	}
	if len(terms) == 0 {
		return nil
	}
	t := strings.Join(terms, " & ")
	return &t
}

func likePattern(q string) *string {
	p := likeEscaper.Replace(q)
	return &p
}
