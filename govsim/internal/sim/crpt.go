package sim

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Verdict — ответ разрешительного режима по коду: можно ли продать и почему нет.
type Verdict struct {
	Code      string     `json:"code"`
	Allowed   bool       `json:"allowed"`
	Reason    string     `json:"reason,omitempty"` // code.unknown, code.sold, code.blocked, code.expired, code.not_owner
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CheckCodes проверяет коды перед продажей (ИС-3). Код — GTIN и серийный номер.
func (s *Sim) CheckCodes(ctx context.Context, fsrar string, codes []string) ([]Verdict, error) {
	out := make([]Verdict, len(codes))
	for i, c := range codes {
		v := Verdict{Code: c}
		var owner, status string
		var expires time.Time
		err := s.pool.QueryRow(ctx, `select owner_fsrar, status, expires_at from crpt_codes where code = $1`, c).
			Scan(&owner, &status, &expires)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			v.Reason = "code.unknown"
		case err != nil:
			return nil, err
		case status == "sold":
			v.Reason = "code.sold"
		case status == "blocked":
			v.Reason = "code.blocked"
		case expires.Before(time.Now().Truncate(24 * time.Hour)):
			v.Reason = "code.expired"
		case owner != fsrar:
			v.Reason = "code.not_owner"
		default:
			v.Allowed = true
		}
		if err == nil {
			v.ExpiresAt = &expires
		}
		out[i] = v
	}
	return out, nil
}

// SellCodes выводит проданные коды из оборота. Повтор безопасен.
func (s *Sim) SellCodes(ctx context.Context, fsrar string, codes []string) error {
	_, err := s.pool.Exec(ctx, `update crpt_codes set status = 'sold'
		where code = any($1) and owner_fsrar = $2 and status = 'introduced'`, codes, fsrar)
	return err
}

// ReturnCodes возвращает коды в оборот при возврате покупателем.
func (s *Sim) ReturnCodes(ctx context.Context, fsrar string, codes []string) error {
	_, err := s.pool.Exec(ctx, `update crpt_codes set status = 'introduced'
		where code = any($1) and owner_fsrar = $2 and status = 'sold'`, codes, fsrar)
	return err
}

// CodeInfo — карточка кода для веб-интерфейса.
type CodeInfo struct {
	Code, GTIN, Serial string
	Name               *string
	Owner, Status      string
	ProducedAt         time.Time
	ExpiresAt          time.Time
	Batch              string
	BlockReason        *string
}

func (s *Sim) Code(ctx context.Context, code string) (CodeInfo, error) {
	rows, err := s.pool.Query(ctx, `select c.code, c.gtin, c.serial, p.name, c.owner_fsrar, c.status, c.produced_at,
		c.expires_at, c.batch, c.block_reason from crpt_codes c left join products p using (alcocode) where c.code = $1`, code)
	if err != nil {
		return CodeInfo{}, err
	}
	info, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[CodeInfo])
	if errors.Is(err, pgx.ErrNoRows) {
		return info, ErrNotFound
	}
	return info, err
}

// Batch — партия кодов: сценарий «партия просрочена или отозвана» действует на неё целиком.
type Batch struct {
	Batch   string
	Name    *string
	Codes   int
	Blocked int
	Expires time.Time
}

func (s *Sim) Batches(ctx context.Context) ([]Batch, error) {
	rows, err := s.pool.Query(ctx, `select c.batch, max(p.name), count(*)::int, count(*) filter (where c.status = 'blocked')::int,
		min(c.expires_at) from crpt_codes c left join products p using (alcocode) group by c.batch order by c.batch desc limit 100`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Batch])
}

// BlockBatch отзывает партию: проданные коды остаются проданными.
func (s *Sim) BlockBatch(ctx context.Context, batch, reason string) error {
	_, err := s.pool.Exec(ctx, `update crpt_codes set status = 'blocked', block_reason = $2
		where batch = $1 and status = 'introduced'`, batch, reason)
	return err
}

// ExpireBatch делает срок годности партии вчерашним.
func (s *Sim) ExpireBatch(ctx context.Context, batch string) error {
	_, err := s.pool.Exec(ctx, `update crpt_codes set expires_at = current_date - 1 where batch = $1`, batch)
	return err
}
