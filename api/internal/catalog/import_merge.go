package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

// ImportChange — расхождение файла с каталогом у позиции или варианта, которые уже есть.
// Как конфликт при слиянии веток: пользователь решает, оставить значение каталога или взять
// из файла. Key — строка и поле — одинаков при предпросмотре и загрузке того же файла.
type ImportChange struct {
	Key, Product, Variant, Field, Old, New string
	Row                                    int
}

// change запоминает расхождение и говорит, принял ли его пользователь. Без решения
// остаётся значение каталога.
func (g *groupImport) change(r importRow, product, variant, code, field, old, new string) bool {
	k := fmt.Sprintf("%d:%s", r.num, code)
	g.changes = append(g.changes, ImportChange{
		Key: k, Row: r.num, Product: product, Variant: variant, Field: field, Old: old, New: new,
	})
	return g.accept[k]
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	return *s
}

func formatAbv(v *float64) string {
	if v == nil {
		return "—"
	}
	return strings.Replace(strconv.FormatFloat(*v, 'f', -1, 64), ".", ",", 1) + " %"
}

func nonEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func yesNoText(b bool) string { return map[bool]string{true: "да", false: "нет"}[b] }

func lastSegment(path string) string {
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func cardFields(c store.ProductCard) apispec.ProductFields {
	attrs := map[string]any{}
	_ = json.Unmarshal(c.Attributes, &attrs)
	return apispec.ProductFields{
		Name: c.Name, CategoryID: c.CategoryID, KindCode: c.KindCode, Abv: c.Abv, CrptMarked: c.CrptMarked,
		ProducerID: c.ProducerID, BrandID: c.BrandID, ProductLineID: c.ProductLineID, Variety: c.Variety,
		CountryCode: c.CountryCode, OriginRegionID: c.OriginRegionID, Attributes: attrs,
	}
}

// sameAttributes сравнивает характеристики по значениям: encoding/json сортирует ключи.
func sameAttributes(a, b map[string]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// mergeProduct сравнивает позицию из файла с позицией каталога и применяет принятые изменения.
func (g *groupImport) mergeProduct(ctx context.Context, id uuid.UUID, f apispec.ProductFields, r importRow) (store.ProductCard, error) {
	card, err := g.svc.q.GetProduct(ctx, id)
	if err != nil {
		return card, err
	}
	names := map[string]string{}
	for _, category := range []uuid.UUID{card.CategoryID, f.CategoryID} {
		schema, err := g.schema(ctx, category)
		if err != nil {
			return card, err
		}
		for _, a := range schema {
			names[a.Code] = a.Name
		}
	}
	current := cardFields(card)
	merged := current
	accepted := false
	v := r.v
	diff := func(differ bool, code, field, old, new string, apply func()) {
		if differ && g.change(r, card.Name, "", code, field, old, new) {
			apply()
			accepted = true
		}
	}
	abv := f.Abv
	if abv != nil {
		rounded := math.Round(*abv*10) / 10
		abv = &rounded
	}
	diff(card.CategoryID != f.CategoryID, "category", "Категория", card.CategoryName, lastSegment(v["category"]),
		func() { merged.CategoryID = f.CategoryID })
	diff(!samePtr(card.KindCode, f.KindCode), "kind", "Вид продукции", orDash(card.KindCode), orDash(f.KindCode),
		func() { merged.KindCode = f.KindCode })
	diff(!samePtr(card.Abv, abv), "abv", "Крепость", formatAbv(card.Abv), formatAbv(abv),
		func() { merged.Abv = abv })
	diff(card.CrptMarked != f.CrptMarked, "crpt", "Честный знак", yesNoText(card.CrptMarked), yesNoText(f.CrptMarked),
		func() { merged.CrptMarked = f.CrptMarked })
	diff(!samePtr(card.BrandID, f.BrandID), "brand", "Бренд", orDash(card.BrandName), orDash(textOrNil(v["brand"])),
		func() { merged.BrandID, merged.ProductLineID = f.BrandID, f.ProductLineID })
	diff(samePtr(card.BrandID, f.BrandID) && !samePtr(card.ProductLineID, f.ProductLineID), "line", "Линейка",
		orDash(card.LineName), orDash(textOrNil(v["line"])), func() { merged.ProductLineID = f.ProductLineID })
	diff(!samePtr(card.Variety, f.Variety), "variety", "Разновидность", orDash(card.Variety), orDash(f.Variety),
		func() { merged.Variety = f.Variety })
	diff(card.CountryCode != f.CountryCode, "country", "Страна", card.CountryName, v["country"],
		func() { merged.CountryCode, merged.OriginRegionID = f.CountryCode, f.OriginRegionID })
	diff(card.CountryCode == f.CountryCode && !samePtr(card.OriginRegionID, f.OriginRegionID), "region",
		"Регион происхождения", orDash(card.RegionName), orDash(textOrNil(v["region"])),
		func() { merged.OriginRegionID = f.OriginRegionID })
	attrs, _ := json.Marshal(f.Attributes)
	diff(!sameAttributes(current.Attributes, f.Attributes), "attributes", "Характеристики",
		nonEmpty(describeAttributes(card.Attributes, names)), nonEmpty(describeAttributes(attrs, names)),
		func() { merged.Attributes = f.Attributes })

	if !accepted {
		return card, nil
	}
	return g.svc.UpdateProduct(ctx, id, merged, card.IsActive, card.Version)
}

// mergeVariant сверяет коды и цены варианта, который уже есть. Новый штрихкод просто
// добавляется: у варианта их может быть несколько.
func (g *groupImport) mergeVariant(ctx context.Context, product store.ProductCard, v Variant, spec VariantSpec, r importRow, prices [2]*int64) error {
	label := variantLabel(v.Variant)
	alcocode, gtin, active := v.Alcocode, v.Gtin, v.IsActive
	codesChanged := false
	if spec.Alcocode != nil && !samePtr(v.Alcocode, spec.Alcocode) &&
		g.change(r, product.Name, label, "alcocode", "Алкокод", orDash(v.Alcocode), *spec.Alcocode) {
		alcocode, codesChanged = spec.Alcocode, true
	}
	if spec.GTIN != nil && !samePtr(v.Gtin, spec.GTIN) &&
		g.change(r, product.Name, label, "gtin", "GTIN", orDash(v.Gtin), *spec.GTIN) {
		gtin, codesChanged = spec.GTIN, true
	}
	if codesChanged {
		if _, err := g.svc.UpdateVariant(ctx, v.ID, alcocode, gtin, active); err != nil {
			return err
		}
	}
	for i, p := range []struct {
		code, field string
		kind        apispec.PriceKind
		current     *int64
	}{
		{"price", "Цена", apispec.PriceKindRetail, v.RetailPrice},
		{"purchase", "Закупочная цена", apispec.PriceKindPurchase, v.PurchasePrice},
	} {
		next := prices[i]
		if next == nil || samePtr(p.current, next) {
			continue
		}
		old := "—"
		if p.current != nil {
			old = rubles(*p.current)
		}
		if g.change(r, product.Name, label, p.code, p.field, old, rubles(*next)) {
			if _, err := g.svc.SetPrice(ctx, v.ID, p.kind, *next, nil, g.setBy); err != nil {
				return err
			}
		}
	}
	return nil
}
