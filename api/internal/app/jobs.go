package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/ADEXITUM/pikni/api/internal/notify"
	"github.com/ADEXITUM/pikni/api/internal/reconcile"
	"github.com/ADEXITUM/pikni/api/internal/reports"
)

// Периодические задачи. River выбирает ведущий экземпляр API, поэтому при нескольких
// экземплярах каждая задача выполняется один раз, а при отказе ведущего её подхватит другой.
type (
	notifyArgs    struct{}
	sweepArgs     struct{}
	dailyRestArgs struct{}
	cleanupArgs   struct{}
	networkArgs   struct{}
	integrityArgs struct{}
)

func (notifyArgs) Kind() string    { return "notify" }
func (sweepArgs) Kind() string     { return "reservations_sweep" }
func (dailyRestArgs) Kind() string { return "egais_daily_rests" }
func (cleanupArgs) Kind() string   { return "report_cleanup" }
func (networkArgs) Kind() string   { return "network_stock" }
func (integrityArgs) Kind() string { return "stock_integrity" }

// task — работник River из обычной функции.
type task[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	run func(context.Context) error
	// timeout — свой предел вместо минуты River по умолчанию.
	timeout time.Duration
}

func (t *task[T]) Work(ctx context.Context, _ *river.Job[T]) error { return t.run(ctx) }

func (t *task[T]) Timeout(*river.Job[T]) time.Duration { return t.timeout }

// traceJob — каждая задача начинает свою трассировку: её запросы к базе и внешним системам
// видны одним деревом, как у HTTP-запроса.
var traceJob = river.WorkerMiddlewareFunc(func(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	ctx, span := otel.Tracer("github.com/ADEXITUM/pikni/api/internal/app").Start(ctx, "job "+job.Kind,
		trace.WithNewRoot(), trace.WithAttributes(attribute.Int64("river.job_id", job.ID), attribute.Int("river.attempt", job.Attempt)))
	defer span.End()
	err := doInner(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return err
})

func every[T river.JobArgs](interval time.Duration, args T) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			// Одна задача за интервал: если ведущий сменился, дубль не встанет в очередь.
			return args, &river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: interval}}
		},
		&river.PeriodicJobOpts{RunOnStart: true})
}

// Jobs собирает фоновые задачи: отчёты по запросу, уведомления, снятие броней, ежедневный
// запрос остатков ЕГАИС (если подключён УТМ) и очистку «Моих отчётов».
func Jobs(pool *pgxpool.Pool, log *slog.Logger, egais bool) (*river.Client[pgx.Tx], error) {
	rep, err := reports.NewService(pool)
	if err != nil {
		return nil, err
	}
	notes := notify.NewService(pool)
	workers := river.NewWorkers()
	river.AddWorker(workers, &reports.Worker{Svc: rep})
	river.AddWorker(workers, &task[notifyArgs]{run: func(ctx context.Context) error {
		_, err := notes.Scan(ctx)
		return err
	}})
	river.AddWorker(workers, &task[sweepArgs]{run: Reservations(pool).Sweep})
	river.AddWorker(workers, &task[dailyRestArgs]{run: reconcile.NewService(pool).RequestDaily})
	river.AddWorker(workers, &task[cleanupArgs]{run: rep.Cleanup})
	// Целостность остатков для «Состояния системы» — раз в час: на большой сети это минуты.
	river.AddWorker(workers, &task[integrityArgs]{run: func(ctx context.Context) error {
		start := time.Now()
		var problems int
		if err := pool.QueryRow(ctx, "select count(*) from stock_integrity()").Scan(&problems); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, "insert into integrity_checks (problems, duration_ms) values ($1, $2)",
			problems, time.Since(start).Milliseconds())
		return err
	}, timeout: 30 * time.Minute})
	// Остаток по сети для «Наличия» — раз в минуту (решение 0064).
	river.AddWorker(workers, &task[networkArgs]{run: func(ctx context.Context) error {
		_, err := pool.Exec(ctx, "select refresh_network_stock()")
		return err
	}})

	periodic := []*river.PeriodicJob{
		every(time.Minute, notifyArgs{}),
		every(time.Minute, sweepArgs{}),
		every(time.Hour, cleanupArgs{}),
		every(time.Minute, networkArgs{}),
		every(time.Hour, integrityArgs{}),
	}
	if egais {
		periodic = append(periodic, every(time.Hour, dailyRestArgs{}))
	}
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:       log,
		Middleware:   []rivertype.Middleware{traceJob},
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers:      workers,
		PeriodicJobs: periodic,
	})
}
