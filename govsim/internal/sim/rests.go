package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
)

// RestsFile — остатки точек, как их выгружает генератор демо-данных «Пикни» (seed-demo-year -rests).
type RestsFile struct {
	Points []struct {
		FSRARID string      `json:"fsrar_id"`
		Marks   [][2]string `json:"marks"`
		Rests   []struct {
			Alcocode string `json:"alcocode"`
			Register int16  `json:"register"`
			Qty      int32  `json:"qty"`
		} `json:"rests"`
	} `json:"points"`
}

// SeedRests ставит на баланс точек ЕГАИС то, что числится у них в учёте после генерации демо-года:
// без этого продажа или перемещение демо-бутылки получили бы отказ «марки нет на балансе».
// Прежние марки и остатки этих точек заменяются.
func (s *Sim) SeedRests(ctx context.Context, r io.Reader) (marks, rests int, err error) {
	var f RestsFile
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return 0, 0, fmt.Errorf("rests: %w", err)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, p := range f.Points {
			if _, err := tx.Exec(ctx, `delete from mark_events where code in (select code from marks where owner_fsrar = $1)`, p.FSRARID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `delete from marks where owner_fsrar = $1`, p.FSRARID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `delete from rests where fsrar_id = $1`, p.FSRARID); err != nil {
				return err
			}
			rows := make([][]any, 0, len(p.Marks))
			for _, m := range p.Marks {
				rows = append(rows, []any{m[0], m[1], p.FSRARID, "on_balance"})
			}
			n, err := tx.CopyFrom(ctx, pgx.Identifier{"marks"}, []string{"code", "alcocode", "owner_fsrar", "status"}, pgx.CopyFromRows(rows))
			if err != nil {
				return err
			}
			marks += int(n)
			rows = rows[:0]
			for _, r := range p.Rests {
				rows = append(rows, []any{p.FSRARID, r.Alcocode, r.Register, r.Qty})
			}
			n, err = tx.CopyFrom(ctx, pgx.Identifier{"rests"}, []string{"fsrar_id", "alcocode", "register", "qty"}, pgx.CopyFromRows(rows))
			if err != nil {
				return err
			}
			rests += int(n)
		}
		return nil
	})
	return marks, rests, err
}
