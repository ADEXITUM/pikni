// Package reconcile — сверка с ЕГАИС (СО-9): система запрашивает остатки точки в ЕГАИС (склад,
// торговый зал, марки) и сравнивает их со своим учётом. Ежедневно — сама, по запросу — кнопкой.
package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/egais"
)

var (
	ErrNotFound = errors.New("location not found")
	ErrNoFSRAR  = errors.New("location has no FSRAR ID")
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Request ставит в очередь запрос остатков; ответ УТМ придёт обменом и сохранится снимком.
func (s *Service) Request(ctx context.Context, location uuid.UUID) error {
	if err := auth.CheckLocation(ctx, apispec.PermissionEgaisReconcile, location); err != nil {
		return err
	}
	var fsrar *string
	err := s.pool.QueryRow(ctx, `select fsrar_id from locations where id = $1`, location).Scan(&fsrar)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if fsrar == nil {
		return ErrNoFSRAR
	}
	return egais.EnqueueStandalone(ctx, s.pool, location, egais.QueryRests, map[string]any{})
}

// Product — продукция без марок на регистре ЕГАИС: 1 — склад, 2 — торговый зал.
type Product struct {
	Alcocode, Name string
	Register       int
	Ours, Egais    int
}

// Mark — марка, которая есть только с одной стороны. У марки, которой нет на остатке точки,
// Status и документ — что о ней знает система: например, в пути по перемещению.
type Mark struct {
	Code, Alcocode, Name string
	OnlyOurs             bool
	Status               *string
	DocumentID           *uuid.UUID
	DocumentNumber       *string
}

type Report struct {
	TakenAt     *time.Time
	RequestedAt *time.Time
	Products    []Product
	Marks       []Mark
}

type snapshot struct {
	Rests []struct {
		Alcocode string `json:"alcocode"`
		Name     string `json:"name"`
		Register int    `json:"register"`
		Qty      int    `json:"qty"`
	} `json:"rests"`
	Marks []struct {
		Code     string `json:"code"`
		Alcocode string `json:"alcocode"`
		Name     string `json:"name"`
	} `json:"marks"`
}

// Report сравнивает последний снимок остатков ЕГАИС с учётом точки. В отчёт попадают только
// расхождения.
func (s *Service) Report(ctx context.Context, location uuid.UUID) (Report, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionEgaisReconcile, location); err != nil {
		return Report{}, err
	}
	return s.Differences(ctx, location)
}

// Differences — то же сравнение без проверки прав: для фоновых уведомлений о расхождениях.
func (s *Service) Differences(ctx context.Context, location uuid.UUID) (Report, error) {
	var r Report
	_ = s.pool.QueryRow(ctx, `select max(created_at) from egais_messages where location_id = $1 and doc_type = 'QueryRests'`,
		location).Scan(&r.RequestedAt)
	var raw []byte
	var taken time.Time
	err := s.pool.QueryRow(ctx, `select payload, taken_at from egais_rests_snapshots where location_id = $1
		order by taken_at desc limit 1`, location).Scan(&raw, &taken)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	r.TakenAt = &taken
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return r, err
	}

	type key struct {
		alcocode string
		register int
	}
	products := map[key]*Product{}
	for _, it := range snap.Rests {
		products[key{it.Alcocode, it.Register}] = &Product{Alcocode: it.Alcocode, Name: it.Name, Register: it.Register, Egais: it.Qty}
	}
	rows, err := s.pool.Query(ctx, `
		select v.alcocode, p.name, case when z.kind = 'floor' then 2 else 1 end as register, sum(b.on_hand)::integer
		from stock_balances b
		join zones z on z.id = b.zone_id
		join variants v on v.id = b.variant_id
		join products p on p.id = v.product_id
		left join product_kinds k on k.code = p.kind_code
		where b.location_id = $1 and v.alcocode is not null and coalesce(k.egais_accounting, '') = 'quantity'
		group by v.alcocode, p.name, register`, location)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.Alcocode, &p.Name, &p.Register, &p.Ours); err != nil {
			return r, err
		}
		if got, ok := products[key{p.Alcocode, p.Register}]; ok {
			got.Ours += p.Ours
			got.Name = p.Name
		} else {
			products[key{p.Alcocode, p.Register}] = &p
		}
	}
	if err := rows.Err(); err != nil {
		return r, err
	}
	for _, p := range products {
		if p.Ours != p.Egais {
			r.Products = append(r.Products, *p)
		}
	}
	sort.Slice(r.Products, func(i, j int) bool {
		if r.Products[i].Register != r.Products[j].Register {
			return r.Products[i].Register < r.Products[j].Register
		}
		return r.Products[i].Name < r.Products[j].Name
	})

	egaisMarks := map[string]Mark{}
	for _, m := range snap.Marks {
		egaisMarks[m.Code] = Mark{Code: m.Code, Alcocode: m.Alcocode, Name: m.Name}
	}
	mrows, err := s.pool.Query(ctx, `
		select e.code, coalesce(v.alcocode, ''), p.name
		from excise_marks e
		join variants v on v.id = e.variant_id
		join products p on p.id = v.product_id
		where e.location_id = $1 and e.status in ('in_stock', 'reserved')`, location)
	if err != nil {
		return r, err
	}
	for mrows.Next() {
		var m Mark
		if err := mrows.Scan(&m.Code, &m.Alcocode, &m.Name); err != nil {
			return r, err
		}
		if _, ok := egaisMarks[m.Code]; ok {
			delete(egaisMarks, m.Code)
			continue
		}
		m.OnlyOurs = true
		r.Marks = append(r.Marks, m)
	}
	if err := mrows.Err(); err != nil {
		return r, err
	}
	codes := make([]string, 0, len(egaisMarks))
	for code := range egaisMarks {
		codes = append(codes, code)
	}
	known, err := s.pool.Query(ctx, `select e.code, e.status, d.id, d.number from excise_marks e
		left join documents d on d.id = e.last_document_id where e.code = any($1)`, codes)
	if err != nil {
		return r, err
	}
	for known.Next() {
		var code string
		var status *string
		var doc *uuid.UUID
		var number *string
		if err := known.Scan(&code, &status, &doc, &number); err != nil {
			return r, err
		}
		m := egaisMarks[code]
		m.Status, m.DocumentID, m.DocumentNumber = status, doc, number
		egaisMarks[code] = m
	}
	if err := known.Err(); err != nil {
		return r, err
	}
	for _, m := range egaisMarks {
		r.Marks = append(r.Marks, m)
	}
	sort.Slice(r.Marks, func(i, j int) bool { return r.Marks[i].Code < r.Marks[j].Code })
	return r, nil
}

// RequestDaily запрашивает остатки точек, у которых снимку больше суток (ежедневная сверка). Её
// запускает фоновая задача раз в час.
func (s *Service) RequestDaily(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		insert into egais_messages (location_id, direction, doc_type, payload)
		select l.id, 'out', 'QueryRests', '{}'
		from locations l
		where l.is_active and l.fsrar_id is not null
		  and not exists (select 1 from egais_rests_snapshots r where r.location_id = l.id and r.taken_at > now() - interval '1 day')
		  and not exists (select 1 from egais_messages m where m.location_id = l.id and m.doc_type = 'QueryRests'
		                  and m.created_at > now() - interval '1 day')`)
	return err
}
