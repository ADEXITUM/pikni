// Package stock — регистр движений, остатки по зонам и средняя себестоимость (СО-7).
// Остатки меняются только движениями: Apply вызывает модуль документов в транзакции проведения.
package stock

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/stock/store"
)

// Move — изменение остатка варианта в зоне по строке документа.
type Move struct {
	LineID     uuid.UUID // uuid.Nil — движение без строки
	VariantID  uuid.UUID
	LocationID uuid.UUID
	ZoneID     uuid.UUID
	// Qty со знаком: плюс — приход в зону, минус — расход из зоны.
	Qty int32
	// External — движение меняет запас организации: поступление, ввод остатков, продажа, списание.
	// Передача в зал и перемещение только переносят запас между зонами и среднюю не меняют.
	External bool
	// UnitCostE4 — цена внешнего прихода за единицу в десятитысячных рубля.
	UnitCostE4 int64
	// AtAverage — внешний приход по текущей средней, без её пересчёта: возврат покупателя.
	AtAverage bool
	// AllowDeficit — продажа через кассу сверх учёта (решение 0013): то, чего нет на остатке,
	// записывается в недостачу к разбору, а не отклоняется.
	AllowDeficit bool
	// InTransit — движение запаса «в пути» зоны получателя (перемещение между точками, СО-4):
	// приход при отгрузке, расход при приёмке. Запас организации и средняя не меняются.
	InTransit bool
}

// Doc — документ, которым проводятся движения.
type Doc struct {
	ID   uuid.UUID
	Date time.Time
}

// ShortageError — расход больше свободного остатка зоны.
type ShortageError struct {
	VariantID, ZoneID uuid.UUID
	Available, Need   int32
}

func (e *ShortageError) Error() string {
	return fmt.Sprintf("not enough stock of %s in zone %s: %d available, %d needed", e.VariantID, e.ZoneID, e.Available, e.Need)
}

// BlockedError — товар входит в идущую ревизию точки (СО-6): движения по нему ждут её завершения.
type BlockedError struct {
	VariantID uuid.UUID
	Stocktake string
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("variant %s is counted by stocktake %s", e.VariantID, e.Stocktake)
}

// checkBlocked проверяет все пары движения одним запросом: ревизия, которая сама проводит
// движения, себя не блокирует.
func checkBlocked(ctx context.Context, tx pgx.Tx, doc uuid.UUID, moves []Move) error {
	var locations, zones, variants []uuid.UUID
	for _, m := range moves {
		locations, zones, variants = append(locations, m.LocationID), append(zones, m.ZoneID), append(variants, m.VariantID)
	}
	var variant uuid.UUID
	var number string
	// Продажа во время подсчёта разрешена, если ревизия не останавливает продажи (решение 0060).
	err := tx.QueryRow(ctx, `select t.variant_id, b.number
		from (select unnest($1::uuid[]) as location_id, unnest($2::uuid[]) as zone_id, unnest($3::uuid[]) as variant_id) t
		cross join lateral (select stocktake_blocks(t.location_id, t.zone_id, t.variant_id, $4) as number) b
		where b.number is not null
		  and (coalesce((select kind from documents where id = $4), '') <> 'sale' or stocktake_stops_sales(b.number))
		limit 1`, locations, zones, variants, doc).Scan(&variant, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &BlockedError{VariantID: variant, Stocktake: number}
}

type pair struct{ variant, zone uuid.UUID }

type balance struct {
	location                      uuid.UUID
	onHand, reserved, deficit     int32
	inTransit                     int32
	dOnHand, dInTransit, dDeficit int32
}

type cost struct {
	qtyTotal int32
	avgE4    int64
	changed  bool
}

// kopecks переводит сумму «цена в десятитысячных × количество» в копейки с округлением до ближайшей.
func kopecks(unitE4 int64, qty int32) int64 {
	v := unitE4 * int64(qty)
	if v >= 0 {
		return (v + 50) / 100
	}
	return -((-v + 50) / 100)
}

// Apply записывает движения в регистр, меняет остатки и среднюю себестоимость в транзакции tx.
// Возвращает себестоимость единицы по каждой строке документа.
//
// Порядок как в п. 3.2 backend.md: недостающие строки остатков создаются, затем остатки и средние
// блокируются в порядке (вариант, зона) — два документа с общими товарами не заблокируют друг друга.
func Apply(ctx context.Context, tx pgx.Tx, doc Doc, moves []Move) (map[uuid.UUID]int64, error) {
	q := store.New(tx)
	if len(moves) == 0 {
		return map[uuid.UUID]int64{}, nil
	}
	if err := checkBlocked(ctx, tx, doc.ID, moves); err != nil {
		return nil, err
	}

	locations := map[pair]uuid.UUID{}
	var variants []uuid.UUID
	for _, m := range moves {
		locations[pair{m.VariantID, m.ZoneID}] = m.LocationID
		if !slices.Contains(variants, m.VariantID) {
			variants = append(variants, m.VariantID)
		}
	}
	pairs := make([]pair, 0, len(locations))
	for p := range locations {
		pairs = append(pairs, p)
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		if c := slices.Compare(a.variant[:], b.variant[:]); c != 0 {
			return c
		}
		return slices.Compare(a.zone[:], b.zone[:])
	})
	slices.SortFunc(variants, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

	ensure := store.EnsureBalancesParams{}
	lock := store.LockBalancesParams{}
	for _, p := range pairs {
		ensure.VariantIds = append(ensure.VariantIds, p.variant)
		ensure.ZoneIds = append(ensure.ZoneIds, p.zone)
		ensure.LocationIDs = append(ensure.LocationIDs, locations[p])
		lock.VariantIds = append(lock.VariantIds, p.variant)
		lock.ZoneIds = append(lock.ZoneIds, p.zone)
	}
	if err := q.EnsureBalances(ctx, ensure); err != nil {
		return nil, err
	}
	rows, err := q.LockBalances(ctx, lock)
	if err != nil {
		return nil, err
	}
	balances := make(map[pair]*balance, len(rows))
	for _, r := range rows {
		p := pair{r.VariantID, r.ZoneID}
		balances[p] = &balance{location: locations[p], onHand: r.OnHand, reserved: r.Reserved, deficit: r.Deficit, inTransit: r.InTransit}
	}

	if err := q.EnsureCosts(ctx, variants); err != nil {
		return nil, err
	}
	costRows, err := q.LockCosts(ctx, variants)
	if err != nil {
		return nil, err
	}
	costs := make(map[uuid.UUID]*cost, len(costRows))
	for _, r := range costRows {
		costs[r.VariantID] = &cost{qtyTotal: r.QtyTotal, avgE4: r.AvgCostE4}
	}

	var out store.InsertMovesParams
	record := func(m Move, bucket string, qty int32, amount int64) {
		out.LineIds = append(out.LineIds, m.LineID)
		out.VariantIds = append(out.VariantIds, m.VariantID)
		out.LocationIDs = append(out.LocationIDs, m.LocationID)
		out.ZoneIds = append(out.ZoneIds, m.ZoneID)
		out.Buckets = append(out.Buckets, bucket)
		out.Qtys = append(out.Qtys, qty)
		out.CostKopecks = append(out.CostKopecks, amount)
	}

	unitCosts := make(map[uuid.UUID]int64, len(moves))
	for _, m := range moves {
		b, c := balances[pair{m.VariantID, m.ZoneID}], costs[m.VariantID]
		var unit int64
		switch {
		case m.InTransit:
			unit = c.avgE4
			if m.Qty < 0 && -m.Qty > b.inTransit {
				return nil, &ShortageError{VariantID: m.VariantID, ZoneID: m.ZoneID, Available: b.inTransit, Need: -m.Qty}
			}
			b.inTransit += m.Qty
			b.dInTransit += m.Qty
			record(m, "in_transit", m.Qty, kopecks(unit, m.Qty))
			// Списание недостачи «в пути» уменьшает запас организации.
			if m.External {
				c.qtyTotal += m.Qty
				c.changed = true
			}

		case m.Qty < 0:
			need := -m.Qty
			free := max(b.onHand-b.reserved, 0)
			if need > free && !m.AllowDeficit {
				return nil, &ShortageError{VariantID: m.VariantID, ZoneID: m.ZoneID, Available: free, Need: need}
			}
			unit = c.avgE4
			take := min(need, free)
			if take > 0 {
				b.onHand -= take
				b.dOnHand -= take
				record(m, "on_hand", -take, -kopecks(unit, take))
			}
			if short := need - take; short > 0 {
				b.deficit += short
				b.dDeficit += short
				record(m, "deficit", short, 0)
			}
			if m.External {
				c.qtyTotal -= need
				c.changed = true
			}

		case m.Qty > 0:
			unit = c.avgE4
			if m.External && m.AtAverage {
				c.qtyTotal += m.Qty
				c.changed = true
			} else if m.External {
				unit = m.UnitCostE4
				// Отрицательный запас в среднюю не входит: приход при нулевом или отрицательном
				// запасе задаёт среднюю ценой прихода (п. 3.3 backend.md).
				if c.qtyTotal <= 0 {
					c.avgE4 = unit
				} else {
					total := int64(c.qtyTotal) + int64(m.Qty)
					c.avgE4 = (int64(c.qtyTotal)*c.avgE4 + int64(m.Qty)*unit + total/2) / total
				}
				c.qtyTotal += m.Qty
				c.changed = true
			}
			// Приход в зону сначала гасит недостачу к разбору: проданное сверх учёта уже ушло
			// покупателю, на остаток встаёт только разница.
			settle := min(b.deficit, m.Qty)
			if settle > 0 {
				b.deficit -= settle
				b.dDeficit -= settle
				record(m, "deficit", -settle, 0)
			}
			if put := m.Qty - settle; put > 0 {
				b.onHand += put
				b.dOnHand += put
				record(m, "on_hand", put, kopecks(unit, put))
			}
		}
		if m.LineID != uuid.Nil {
			unitCosts[m.LineID] = unit
		}
	}

	for _, p := range pairs {
		b := balances[p]
		if b.dOnHand == 0 && b.dInTransit == 0 && b.dDeficit == 0 {
			continue
		}
		if err := q.ChangeBalance(ctx, store.ChangeBalanceParams{
			VariantID: p.variant, ZoneID: p.zone, OnHand: b.dOnHand, InTransit: b.dInTransit, Deficit: b.dDeficit,
		}); err != nil {
			return nil, err
		}
	}
	for _, v := range variants {
		if c := costs[v]; c.changed {
			if err := q.SetCost(ctx, store.SetCostParams{VariantID: v, QtyTotal: c.qtyTotal, AvgCostE4: c.avgE4}); err != nil {
				return nil, err
			}
		}
	}
	out.DocumentID, out.DocDate = doc.ID, doc.Date
	if err := q.InsertMoves(ctx, out); err != nil {
		return nil, err
	}
	return unitCosts, nil
}
