package reports

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/reconcile"
)

// Dashboard — показатели и графики дашборда руководителя (ОТ-4).
type Dashboard struct {
	StockCost, Revenue, Breakage float64
	TurnoverDays                 *float64
	DeadShare                    float64
	EgaisMismatches              int
	UnconfirmedTTN               int
	RevenueByDay                 []Point
	StockByCategory              []Point
	StockByLocation              []Point
}

type Point struct {
	Label string
	Value float64
}

var dashboardRoles = []string{manager, accountant, storeManager}

// Dashboard считает показатели по доступным точкам (или одной выбранной) за период.
func (s *Service) Dashboard(ctx context.Context, from, to time.Time, location *uuid.UUID) (Dashboard, error) {
	var d Dashboard
	sess, ok := auth.FromContext(ctx)
	if !ok || !slices.Contains(dashboardRoles, sess.Role) {
		return d, auth.ErrForbidden
	}
	p := Params{From: from, To: to}
	if location != nil {
		p.Locations = []uuid.UUID{*location}
	}
	if err := p.normalize(Def{Params: []string{"period"}}); err != nil {
		return d, err
	}
	args, err := s.args(ctx, Def{}, p)
	if err != nil {
		return d, err
	}
	days := max(to.Sub(from).Hours()/24+1, 1)
	var cogs float64
	err = s.pool.QueryRow(ctx, `
		select
		  coalesce((select sum(b.on_hand * coalesce(c.avg_cost, 0)) from stock_balances b
		            left join variant_costs c on c.variant_id = b.variant_id
		            where @locs::uuid[] is null or b.location_id = any(@locs::uuid[])), 0)::float8,
		  coalesce((select sum(rl.qty * rl.price) from pos_receipt_lines rl join pos_receipts r on r.id = rl.receipt_id
		            join pos_shifts s on s.id = r.shift_id join pos_devices d on d.id = s.device_id join locations l on l.id = d.location_id
		            where r.created_on_device_at >= @from::date - interval '1 day' and r.created_on_device_at < @to::date + interval '2 days'
		              and (r.created_on_device_at at time zone l.timezone)::date between @from::date and @to::date
		              and (@locs::uuid[] is null or l.id = any(@locs::uuid[]))), 0)::float8,
		  coalesce((select -sum(m.cost_amount) from stock_moves m join documents d on d.id = m.document_id
		            where d.kind = 'sale' and m.bucket = 'on_hand' and m.doc_date between @from::date and @to::date
		              and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))), 0)::float8,
		  coalesce((select -sum(m.cost_amount) from stock_moves m join documents d on d.id = m.document_id
		            join writeoff_reasons wr on wr.id = d.reason_id
		            where d.kind = 'writeoff' and wr.name ilike 'бой%' and m.doc_date between @from::date and @to::date
		              and (@locs::uuid[] is null or m.location_id = any(@locs::uuid[]))), 0)::float8,
		  (select count(*) from documents d where d.kind = 'receipt' and d.status = 'draft' and d.ttn_id is not null
		     and (@locs::uuid[] is null or d.location_id = any(@locs::uuid[])))::int`, args).
		Scan(&d.StockCost, &d.Revenue, &cogs, &d.Breakage, &d.UnconfirmedTTN)
	if err != nil {
		return d, err
	}
	if cogs > 0 {
		t := d.StockCost / (cogs / days)
		d.TurnoverDays = &t
	}
	var dead float64
	if err := s.pool.QueryRow(ctx, `
		select coalesce(sum(b.on_hand * coalesce(c.avg_cost, 0)), 0)::float8
		from stock_balances b left join variant_costs c on c.variant_id = b.variant_id
		where b.on_hand > 0 and (@locs::uuid[] is null or b.location_id = any(@locs::uuid[]))
		  and not exists (select 1 from pos_receipt_lines rl join pos_receipts r on r.id = rl.receipt_id
		                  join pos_shifts s on s.id = r.shift_id join pos_devices dv on dv.id = s.device_id
		                  where rl.variant_id = b.variant_id and dv.location_id = b.location_id and rl.qty > 0
		                    and r.created_on_device_at > now() - interval '90 days')`, args).Scan(&dead); err != nil {
		return d, err
	}
	if d.StockCost > 0 {
		d.DeadShare = dead / d.StockCost * 100
	}
	series := func(sql string) ([]Point, error) {
		rows, err := s.pool.Query(ctx, sql, pgx.QueryExecModeDescribeExec, args)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Point, error) {
			var pt Point
			return pt, r.Scan(&pt.Label, &pt.Value)
		})
	}
	if d.RevenueByDay, err = series(`
		with sales as (
			select (r.created_on_device_at at time zone l.timezone)::date as day, sum(rl.qty * rl.price) as amount
			from pos_receipts r join pos_shifts s on s.id = r.shift_id join pos_devices dv on dv.id = s.device_id
			join locations l on l.id = dv.location_id join pos_receipt_lines rl on rl.receipt_id = r.id
			where r.created_on_device_at >= @from::date - interval '1 day' and r.created_on_device_at < @to::date + interval '2 days'
			  and (r.created_on_device_at at time zone l.timezone)::date between @from::date and @to::date
			  and (@locs::uuid[] is null or l.id = any(@locs::uuid[]))
			group by 1
		)
		select to_char(g.day, 'YYYY-MM-DD'), coalesce(s.amount, 0)::float8
		from generate_series(@from::date, @to::date, interval '1 day') as g(day)
		left join sales s on s.day = g.day
		order by g.day`); err != nil {
		return d, err
	}
	if d.StockByCategory, err = series(`
		select coalesce(gp.name, pc.name, c.name), sum(b.on_hand * coalesce(vc.avg_cost, 0))::float8
		from stock_balances b
		join variants v on v.id = b.variant_id join products p on p.id = v.product_id
		join categories c on c.id = p.category_id
		left join categories pc on pc.id = c.parent_id left join categories gp on gp.id = pc.parent_id
		left join variant_costs vc on vc.variant_id = b.variant_id
		where b.on_hand > 0 and (@locs::uuid[] is null or b.location_id = any(@locs::uuid[]))
		group by 1 order by 2 desc`); err != nil {
		return d, err
	}
	if d.StockByLocation, err = series(`
		select l.code, sum(b.on_hand * coalesce(vc.avg_cost, 0))::float8
		from stock_balances b join locations l on l.id = b.location_id
		left join variant_costs vc on vc.variant_id = b.variant_id
		where b.on_hand > 0 and (@locs::uuid[] is null or b.location_id = any(@locs::uuid[]))
		group by l.code order by 2 desc`); err != nil {
		return d, err
	}
	// Расхождения с ЕГАИС — по последним снимкам остатков точек.
	locs, _ := args["locs"].([]uuid.UUID)
	rows, err := s.pool.Query(ctx, `select id from locations where fsrar_id is not null and is_active
		and ($1::uuid[] is null or id = any($1::uuid[]))`, locs)
	if err != nil {
		return d, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return d, err
	}
	rec := reconcile.NewService(s.pool)
	for _, id := range ids {
		if err := auth.CheckLocation(ctx, apispec.PermissionReportsView, id); err != nil {
			continue
		}
		r, err := rec.Differences(ctx, id)
		if err != nil {
			return d, err
		}
		d.EgaisMismatches += len(r.Products) + len(r.Marks)
	}
	return d, nil
}
