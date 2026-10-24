package catalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrVariantExists    = errors.New("variant already exists")
	ErrVolumeRequired   = errors.New("EGAIS product variant needs a volume")
	ErrVolumeAndWeight  = errors.New("variant has either a volume or a net weight")
	ErrWeightForEgais   = errors.New("EGAIS product packaging is a volume")
	ErrInvalidWeight    = errors.New("net weight must be from 1 g to 100 kg")
	ErrUnknownVolume    = errors.New("volume is not in the reference book")
	ErrInvalidContainer = errors.New("unknown container")
	ErrAlcocodeNotEgais = errors.New("alcocode is only for EGAIS products")
	ErrAlcocodeTaken    = errors.New("alcocode belongs to another variant")
	ErrGTINTaken        = errors.New("GTIN belongs to another variant")
	ErrBarcodeTaken     = errors.New("barcode belongs to another variant")
)

var containerNames = map[string]string{"glass": "стекло", "pet": "ПЭТ", "can": "банка"}

// Variant — вариант со штрихкодами, действующими ценами и МРЦ (в копейках).
type Variant struct {
	store.Variant
	Barcodes                             []store.Barcode
	RetailPrice, PurchasePrice, MinPrice *int64
}

// VariantSpec — что задаёт новый вариант.
type VariantSpec struct {
	VolumeMl       *int32
	NetWeightG     *int32
	Container      *string
	GiftPack       bool
	Alcocode, GTIN *string
}

func (s *Service) Variants(ctx context.Context, productID uuid.UUID) ([]Variant, error) {
	if _, err := s.q.GetVariantProduct(ctx, productID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return variantsOf(ctx, s.q, productID)
}

func variantsOf(ctx context.Context, q *store.Queries, productID uuid.UUID) ([]Variant, error) {
	rows, err := q.ListProductVariants(ctx, productID)
	if err != nil {
		return nil, err
	}
	barcodes, err := q.ListProductBarcodes(ctx, productID)
	if err != nil {
		return nil, err
	}
	prices, err := q.CurrentProductPrices(ctx, productID)
	if err != nil {
		return nil, err
	}
	p, err := q.GetVariantProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	variants := make([]Variant, len(rows))
	for i, v := range rows {
		variants[i] = Variant{Variant: v, Barcodes: []store.Barcode{}}
		for _, b := range barcodes {
			if b.VariantID == v.ID {
				variants[i].Barcodes = append(variants[i].Barcodes, b)
			}
		}
		for _, price := range prices {
			if price.VariantID == v.ID {
				k := price.Kopecks
				if price.Kind == string(apispec.PriceKindRetail) {
					variants[i].RetailPrice = &k
				} else {
					variants[i].PurchasePrice = &k
				}
			}
		}
		if variants[i].MinPrice, err = minRetailPrice(ctx, q, p, v, now); err != nil {
			return nil, err
		}
	}
	return variants, nil
}

func (s *Service) CreateVariant(ctx context.Context, productID uuid.UUID, spec VariantSpec) (Variant, error) {
	created, err := s.createVariants(ctx, productID, []VariantSpec{spec}, false)
	if err != nil {
		return Variant{}, err
	}
	return created[0], nil
}

// GenerateVariants создаёт варианты для всех сочетаний объёма и тары (НСИ-2). Уже
// существующие сочетания пропускаются; пустой список тары — варианты без тары.
func (s *Service) GenerateVariants(ctx context.Context, productID uuid.UUID, volumes []int32, containers []string, giftPack bool) ([]Variant, error) {
	if len(containers) == 0 {
		containers = []string{""}
	}
	var specs []VariantSpec
	for _, ml := range volumes {
		for _, c := range containers {
			spec := VariantSpec{VolumeMl: &ml, GiftPack: giftPack}
			if c != "" {
				spec.Container = &c
			}
			specs = append(specs, spec)
		}
	}
	return s.createVariants(ctx, productID, specs, true)
}

func (s *Service) createVariants(ctx context.Context, productID uuid.UUID, specs []VariantSpec, skipExisting bool) ([]Variant, error) {
	created := []Variant{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		p, err := q.GetVariantProduct(ctx, productID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		existing, err := q.ListProductVariants(ctx, productID)
		if err != nil {
			return err
		}
		for _, spec := range specs {
			if skipExisting && hasVariant(existing, spec) {
				continue
			}
			params, err := variantParams(p, spec)
			if err != nil {
				return err
			}
			v, err := q.CreateVariant(ctx, params)
			if err != nil {
				return variantWriteError(err)
			}
			created = append(created, Variant{Variant: v, Barcodes: []store.Barcode{}})
			err = audit.Record(ctx, tx, audit.Event{
				Action: apispec.AuditActionVariantCreated, EntityType: audit.EntityVariant, EntityID: v.ID.String(),
				Data: map[string]any{"article": v.Article, "name": p.Name, "variant": variantLabel(v)},
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return created, err
}

func hasVariant(existing []store.Variant, spec VariantSpec) bool {
	for _, v := range existing {
		if samePtr(v.VolumeMl, spec.VolumeMl) && samePtr(v.NetWeightG, spec.NetWeightG) &&
			samePtr(v.Container, spec.Container) && v.GiftPack == spec.GiftPack {
			return true
		}
	}
	return false
}

func samePtr[T comparable](a, b *T) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func variantParams(p store.GetVariantProductRow, spec VariantSpec) (store.CreateVariantParams, error) {
	if spec.Container != nil && containerNames[*spec.Container] == "" {
		return store.CreateVariantParams{}, ErrInvalidContainer
	}
	switch {
	case spec.VolumeMl != nil && spec.NetWeightG != nil:
		return store.CreateVariantParams{}, ErrVolumeAndWeight
	case spec.NetWeightG != nil && (*spec.NetWeightG < 1 || *spec.NetWeightG > 100000):
		return store.CreateVariantParams{}, ErrInvalidWeight
	case p.KindCode != nil && spec.NetWeightG != nil:
		return store.CreateVariantParams{}, ErrWeightForEgais
	case p.KindCode != nil && spec.VolumeMl == nil:
		return store.CreateVariantParams{}, ErrVolumeRequired
	}
	alcocode, gtin, err := variantCodes(p, spec.Alcocode, spec.GTIN)
	if err != nil {
		return store.CreateVariantParams{}, err
	}
	return store.CreateVariantParams{
		ProductID: p.ID, VolumeMl: spec.VolumeMl, NetWeightG: spec.NetWeightG, Container: spec.Container, GiftPack: spec.GiftPack,
		Alcocode: alcocode, Gtin: gtin,
	}, nil
}

func variantCodes(p store.GetVariantProductRow, alcocode, gtin *string) (*string, *string, error) {
	a, err := normalizeAlcocode(alcocode)
	if err != nil {
		return nil, nil, err
	}
	if a != nil && p.KindCode == nil {
		return nil, nil, ErrAlcocodeNotEgais
	}
	g, err := normalizeGTIN(gtin)
	return a, g, err
}

func variantWriteError(err error) error {
	switch {
	case db.IsUniqueViolationOn(err, "variants_alcocode_key"):
		return ErrAlcocodeTaken
	case db.IsUniqueViolationOn(err, "variants_gtin_key"):
		return ErrGTINTaken
	case db.IsUniqueViolation(err):
		return ErrVariantExists
	case db.IsForeignKeyViolationOn(err, "variants_volume_ml_fkey"):
		return ErrUnknownVolume
	}
	return fmt.Errorf("write variant: %w", err)
}

func variantLabel(v store.Variant) string {
	return PackLabel(v.VolumeMl, v.NetWeightG, v.Container, v.GiftPack)
}

// PackLabel — подпись фасовки: «0,5 л, стекло, подарочная упаковка», «70 г».
func PackLabel(volumeMl, netWeightG *int32, container *string, giftPack bool) string {
	var parts []string
	decimal := func(n float64) string { return strings.Replace(strconv.FormatFloat(n, 'f', -1, 64), ".", ",", 1) }
	if volumeMl != nil {
		parts = append(parts, decimal(float64(*volumeMl)/1000)+" л")
	}
	switch {
	case netWeightG != nil && *netWeightG >= 1000:
		parts = append(parts, decimal(float64(*netWeightG)/1000)+" кг")
	case netWeightG != nil:
		parts = append(parts, strconv.Itoa(int(*netWeightG))+" г")
	}
	if container != nil {
		parts = append(parts, containerNames[*container])
	}
	if giftPack {
		parts = append(parts, "подарочная упаковка")
	}
	if len(parts) == 0 {
		return "без фасовки"
	}
	return strings.Join(parts, ", ")
}

// UpdateVariant меняет коды и архив. Объём, тара и упаковка не меняются.
func (s *Service) UpdateVariant(ctx context.Context, id uuid.UUID, alcocode, gtin *string, active bool) (Variant, error) {
	var saved Variant
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		old, p, err := lockVariant(ctx, q, id)
		if err != nil {
			return err
		}
		a, g, err := variantCodes(p, alcocode, gtin)
		if err != nil {
			return err
		}
		v, err := q.UpdateVariant(ctx, store.UpdateVariantParams{ID: id, Alcocode: a, Gtin: g, IsActive: active})
		if err != nil {
			return variantWriteError(err)
		}
		fields := func(v store.Variant) map[string]any {
			return map[string]any{"alcocode": v.Alcocode, "gtin": v.Gtin, "isActive": v.IsActive}
		}
		if changes := audit.Changes(fields(old), fields(v)); len(changes) > 0 {
			err = audit.Record(ctx, tx, audit.Event{
				Action: apispec.AuditActionVariantUpdated, EntityType: audit.EntityVariant, EntityID: id.String(),
				Data: map[string]any{"article": v.Article, "name": p.Name, "variant": variantLabel(v), "changes": changes},
			})
			if err != nil {
				return err
			}
		}
		variants, err := variantsOf(ctx, q, v.ProductID)
		for _, x := range variants {
			if x.ID == id {
				saved = x
			}
		}
		return err
	})
	return saved, err
}

func lockVariant(ctx context.Context, q *store.Queries, id uuid.UUID) (store.Variant, store.GetVariantProductRow, error) {
	v, err := q.GetVariant(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, store.GetVariantProductRow{}, ErrNotFound
	}
	if err != nil {
		return v, store.GetVariantProductRow{}, err
	}
	p, err := q.GetVariantProduct(ctx, v.ProductID)
	return v, p, err
}

// AddBarcode привязывает штрихкод к варианту. Без кода создаёт внутренний EAN-13; если такой
// номер кто-то уже ввёл вручную, берёт следующий.
func (s *Service) AddBarcode(ctx context.Context, variantID uuid.UUID, code *string) (store.Barcode, error) {
	var saved store.Barcode
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		v, p, err := lockVariant(ctx, q, variantID)
		if err != nil {
			return err
		}
		if c := optional(code); c != nil {
			kind, err := barcodeKind(*c)
			if err != nil {
				return err
			}
			saved, err = q.CreateBarcode(ctx, store.CreateBarcodeParams{Code: *c, VariantID: variantID, Kind: kind})
			if db.IsUniqueViolation(err) {
				return ErrBarcodeTaken
			}
			if err != nil {
				return err
			}
		} else if saved, err = createInternalBarcode(ctx, tx, q, variantID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionBarcodeCreated, EntityType: audit.EntityVariant, EntityID: variantID.String(),
			Data: map[string]any{"article": v.Article, "name": p.Name, "variant": variantLabel(v), "code": saved.Code},
		})
	})
	return saved, err
}

// Повтор идёт внутри точки сохранения: ошибка вставки не обрывает всю транзакцию.
func createInternalBarcode(ctx context.Context, tx pgx.Tx, q *store.Queries, variantID uuid.UUID) (store.Barcode, error) {
	for range 5 {
		n, err := q.NextInternalBarcode(ctx)
		if err != nil {
			return store.Barcode{}, err
		}
		var b store.Barcode
		err = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			b, err = q.WithTx(sp).CreateBarcode(ctx, store.CreateBarcodeParams{Code: internalBarcode(n), VariantID: variantID, Kind: "internal"})
			return err
		})
		if !db.IsUniqueViolation(err) {
			return b, err
		}
	}
	return store.Barcode{}, ErrBarcodeTaken
}

func (s *Service) DeleteBarcode(ctx context.Context, code string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		b, err := q.DeleteBarcode(ctx, code)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		v, p, err := lockVariant(ctx, q, b.VariantID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionBarcodeDeleted, EntityType: audit.EntityVariant, EntityID: v.ID.String(),
			Data: map[string]any{"article": v.Article, "name": p.Name, "variant": variantLabel(v), "code": b.Code},
		})
	})
}
