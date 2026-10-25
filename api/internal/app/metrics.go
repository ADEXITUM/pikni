package app

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ADEXITUM/pikni/api/internal/platform/httpx"
)

// Metrics регистрирует бизнес-метрики (п. 4.1.5): чеки и сканы — счётчики (в Grafana — в минуту),
// очередь сообщений в ЕГАИС по точкам, недоступные УТМ, недостачи к разбору, очередь задач.
// Значения читаются из базы при каждой выгрузке метрик, то есть раз в минуту.
func Metrics(pool *pgxpool.Pool) error {
	m := otel.Meter("pikni")
	receipts, err := m.Int64ObservableCounter("pikni.receipts", metric.WithDescription("Чеков пробито всего"))
	if err != nil {
		return err
	}
	scans, err := m.Int64ObservableCounter("pikni.scans", metric.WithDescription("Сканов в документах всего"))
	if err != nil {
		return err
	}
	queue, err := m.Int64ObservableGauge("pikni.egais.queue", metric.WithDescription("Сообщений ждут отправки в ЕГАИС"))
	if err != nil {
		return err
	}
	utmDown, err := m.Int64ObservableGauge("pikni.egais.utm_down", metric.WithDescription("Точек, где сообщение ждёт больше 5 минут"))
	if err != nil {
		return err
	}
	deficit, err := m.Int64ObservableGauge("pikni.stock.deficit", metric.WithDescription("Недостачи к разбору, шт."))
	if err != nil {
		return err
	}
	jobs, err := m.Int64ObservableGauge("pikni.jobs.waiting", metric.WithDescription("Фоновых задач в очереди"))
	if err != nil {
		return err
	}
	inFlight, err := m.Int64ObservableGauge("pikni.http.in_flight", metric.WithDescription("Запросов в обработке"))
	if err != nil {
		return err
	}
	conns, err := m.Int64ObservableGauge("pikni.db.connections", metric.WithDescription("Соединения с базой: занятые, свободные, предел пула"))
	if err != nil {
		return err
	}
	waits, err := m.Int64ObservableCounter("pikni.db.acquire_waits", metric.WithDescription("Сколько раз запрос ждал свободного соединения"))
	if err != nil {
		return err
	}
	// Пул и очередь запросов — без обращения к базе, поэтому отдельным обработчиком: они видны,
	// даже когда база перегружена и запрос бизнес-метрик не успевает.
	if _, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		st := pool.Stat()
		o.ObserveInt64(inFlight, httpx.InFlight.Load())
		o.ObserveInt64(conns, int64(st.AcquiredConns()), metric.WithAttributes(attribute.String("state", "acquired")))
		o.ObserveInt64(conns, int64(st.IdleConns()), metric.WithAttributes(attribute.String("state", "idle")))
		o.ObserveInt64(conns, int64(st.MaxConns()), metric.WithAttributes(attribute.String("state", "max")))
		o.ObserveInt64(waits, st.EmptyAcquireCount())
		return nil
	}, inFlight, conns, waits); err != nil {
		return err
	}
	_, err = m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		var r, s, d, j, down int64
		if err := pool.QueryRow(ctx, `select (select count(*) from pos_receipts), (select count(*) from scans),
			(select coalesce(sum(deficit), 0) from stock_balances),
			(select count(*) from river_job where state in ('available', 'retryable')),
			(select count(distinct location_id) from egais_messages where status = 'queued' and attempts > 0
			   and created_at < now() - interval '5 minutes')`).Scan(&r, &s, &d, &j, &down); err != nil {
			return err
		}
		o.ObserveInt64(receipts, r)
		o.ObserveInt64(scans, s)
		o.ObserveInt64(deficit, d)
		o.ObserveInt64(jobs, j)
		o.ObserveInt64(utmDown, down)
		rows, err := pool.Query(ctx, `select l.code, count(m.id) from locations l
			left join egais_messages m on m.location_id = l.id and m.status = 'queued'
			where l.fsrar_id is not null group by l.code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			var n int64
			if err := rows.Scan(&code, &n); err != nil {
				return err
			}
			o.ObserveInt64(queue, n, metric.WithAttributes(attribute.String("location", code)))
		}
		return rows.Err()
	}, receipts, scans, queue, utmDown, deficit, jobs)
	return err
}
