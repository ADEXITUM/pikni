package catalog

import (
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

// Марки и их алкокоды — примеры из документации r_keeper и проекта OpenPapyrus,
// код «Честного знака» — пример из базы знаний markirovka.ru.
const (
	oldMark    = "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891"
	oldMarkTwo = "22N000000IXAIM093V101HE41114002092910TPD01BK4BOQV4QIKXJGU687IYO7TEWZ"
	beerCode   = "0104602124002104215i.7D(U\x1d9333kC"
)

func TestParseScan(t *testing.T) {
	t.Parallel()
	// Марка нового образца: тип, серия, номер, служебные данные ЕГАИС и подпись — всего 150 символов.
	newMark := "101" + "100" + "26167968" + "0118001" + strings.Repeat("A", 129)
	// Тот же скан старой марки, напечатанный сканером в русской раскладке.
	cyrillic := strings.NewReplacer(strings.Split("Q Й W Ц E У R К T Е Y Н U Г I Ш O Щ P З A Ф S Ы D В F А G П H Р J О K Л L Д Z Я X Ч C С V М B И N Т M Ь", " ")...).Replace(oldMark)

	tests := []struct {
		raw  string
		kind apispec.ScanKind
		code string
	}{
		{oldMark, apispec.ScanKindEgaisMark, "0177207000002463655"},
		{oldMarkTwo, apispec.ScanKindEgaisMark, "0001922000001381325"},
		{cyrillic, apispec.ScanKindEgaisMark, "0177207000002463655"},
		{newMark, apispec.ScanKindEgaisMarkNew, "10110026167968"},
		{beerCode, apispec.ScanKindCrptCode, "04602124002104"},
		{"]d2è" + beerCode, apispec.ScanKindCrptCode, "04602124002104"},
		{strings.ReplaceAll(beerCode, "\x1d", ""), apispec.ScanKindCrptCode, "04602124002104"},
		{"0177207000002463655", apispec.ScanKindAlcocode, "0177207000002463655"},
		{" 4006381333931 ", apispec.ScanKindBarcode, "4006381333931"},
		{"PK-BOX-0001", apispec.ScanKindBarcode, "PK-BOX-0001"},
		{"ЧК-000001", apispec.ScanKindArticle, "ЧК-000001"},
		{"чк12", apispec.ScanKindArticle, "ЧК-000012"},
		{"xr-7", apispec.ScanKindArticle, "ЧК-000007"},
	}
	for _, tt := range tests {
		got, ok := parseScan(tt.raw)
		if !ok || got.Kind != tt.kind || got.Code != tt.code {
			t.Errorf("parseScan(%.24q…) = %+v, %v; want %s %s", tt.raw, got, ok, tt.kind, tt.code)
		}
	}
	for _, text := range []string{"тайга", "ЧК-0000001", "000001", "водка 0,5"} {
		if got, ok := parseScan(text); ok {
			t.Errorf("parseScan(%q) = %+v, want plain text", text, got)
		}
	}
}

func TestTsquery(t *testing.T) {
	t.Parallel()
	for q, want := range map[string]string{
		"тайг пшен":       "тайг:* & пшен:*",
		"«Тайга» 0,5 40%": "Тайга:*",
		"Шато-Люмьер":     "Шато:* & Люмьер:*",
	} {
		if got := tsquery(q); got == nil || *got != want {
			t.Errorf("tsquery(%q) = %v, want %q", q, got, want)
		}
	}
	if got := tsquery("0,5 40"); got != nil {
		t.Errorf("tsquery of numbers = %q, want nil", *got)
	}
}
