package reports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/forms"
	"github.com/ADEXITUM/pikni/api/internal/notify"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrNotFound  = errors.New("report not found")
	ErrBadParams = errors.New("invalid report parameters")
	ErrNotReady  = errors.New("report file is not ready")
	ErrTooLarge  = errors.New("report is too large for the screen")
)

// maxRows — отчёт на экране и в файле не больше этого числа строк: больше человек не прочтёт,
// а фильтр сузит выборку.
const maxRows = 20000

type Service struct {
	pool  *pgxpool.Pool
	queue *river.Client[pgx.Tx]
}

func NewService(pool *pgxpool.Pool) (*Service, error) {
	// Клиент только ставит задачи: выполняет их клиент фоновых задач процесса API.
	queue, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, queue: queue}, nil
}

func find(code string) (Def, bool) {
	i := slices.IndexFunc(catalog, func(d Def) bool { return d.Code == code })
	if i < 0 {
		return Def{}, false
	}
	return catalog[i], true
}

func allowed(ctx context.Context, d Def) bool {
	sess, ok := auth.FromContext(ctx)
	return ok && slices.Contains(d.Roles, sess.Role) && auth.Has(ctx, apispec.PermissionReportsView)
}

// Catalog — отчёты, доступные текущему пользователю.
func (s *Service) Catalog(ctx context.Context) []Def {
	var out []Def
	for _, d := range catalog {
		if allowed(ctx, d) {
			out = append(out, d)
		}
	}
	return out
}

// Today — сегодняшняя дата на крайнем востоке России (UTC+12): «сегодня» по умолчанию охватывает
// документы всех точек сети, даже если на сервере ещё вчера.
func Today() time.Time {
	now := time.Now().In(time.FixedZone("UTC+12", 12*3600))
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func (p *Params) normalize(d Def) error {
	need := func(name string) bool { return slices.Contains(d.Params, name) }
	today := Today()
	if need("period") {
		if p.From.IsZero() {
			p.From = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		if p.To.IsZero() {
			p.To = today
		}
		if p.To.Before(p.From) || p.To.Sub(p.From) > 3*366*24*time.Hour {
			return ErrBadParams
		}
	}
	if need("date") && p.Date.IsZero() {
		p.Date = today
	}
	if need("days") && (p.Days <= 0 || p.Days > 3650) {
		p.Days = 90
	}
	if need("group") && !slices.Contains([]string{"location", "category", "brand", "cashier", "day"}, p.Group) {
		p.Group = "location"
	}
	return nil
}

// args — параметры запроса: точки — пересечение выбранных с доступными по праву на отчёты.
func (s *Service) args(ctx context.Context, d Def, p Params) (pgx.NamedArgs, error) {
	scope, err := auth.AllowedLocations(ctx, apispec.PermissionReportsView)
	if err != nil {
		return nil, err
	}
	locs := p.Locations
	switch {
	case scope != nil && len(locs) == 0:
		locs = scope
	case scope != nil:
		locs = slices.DeleteFunc(slices.Clone(locs), func(l uuid.UUID) bool { return !slices.Contains(scope, l) })
		if len(locs) == 0 {
			return nil, auth.ErrForbidden
		}
	}
	if scope != nil && len(locs) == 0 {
		locs = []uuid.UUID{uuid.Nil}
	}
	sess, _ := auth.FromContext(ctx)
	var own *uuid.UUID
	if d.Own && sess.Role == cashier {
		own = &sess.UserID
	}
	nilIfEmpty := func(ids []uuid.UUID) []uuid.UUID {
		if len(ids) == 0 {
			return nil
		}
		return ids
	}
	return pgx.NamedArgs{
		"from": p.From, "to": p.To, "date": p.Date, "days": p.Days, "group": p.Group,
		"locs": nilIfEmpty(locs), "cats": nilIfEmpty(p.Categories), "brands": nilIfEmpty(p.Brands), "me": own,
	}, nil
}

// Run строит отчёт.
func (s *Service) Run(ctx context.Context, code string, p Params) (Def, Table, error) {
	d, ok := find(code)
	if !ok || !allowed(ctx, d) {
		return d, Table{}, ErrNotFound
	}
	if err := p.normalize(d); err != nil {
		return d, Table{}, err
	}
	args, err := s.args(ctx, d, p)
	if err != nil {
		return d, Table{}, err
	}
	// На экране отчёт ждут, поэтому время ограничено: отчёт по большой сети целиком выгружают файлом,
	// он строится в фоне (решение 0064).
	qctx, cancel := context.WithTimeout(ctx, screenTimeout)
	defer cancel()
	// План — под выбранные точки и период: общий план кэшированного запроса с условием «точки не
	// выбраны или точка из списка» не использует индекс и читает журнал всей сети.
	rows, err := s.pool.Query(qctx, d.SQL, pgx.QueryExecModeDescribeExec, args)
	if err != nil {
		if qctx.Err() != nil && ctx.Err() == nil {
			return d, Table{}, ErrTooLarge
		}
		return d, Table{}, fmt.Errorf("report %s: %w", code, err)
	}
	defer rows.Close()
	t := Table{Title: d.Title, Columns: d.Columns}
	for rows.Next() && len(t.Rows) < maxRows {
		vals, err := rows.Values()
		if err != nil {
			return d, t, err
		}
		for i, v := range vals {
			vals[i] = plain(v)
		}
		t.Rows = append(t.Rows, vals)
	}
	if err := rows.Err(); err != nil && qctx.Err() != nil && ctx.Err() == nil {
		return d, t, ErrTooLarge
	}
	return d, t, rows.Err()
}

const screenTimeout = 20 * time.Second

// Background — выгрузку строить в фоне: тяжёлый отчёт за период длиннее месяца или отчёт по всей
// сети, когда в ней больше 50 точек.
func (s *Service) Background(ctx context.Context, code string, p Params) bool {
	d, ok := find(code)
	if !ok {
		return false
	}
	if d.Heavy && (p.From.IsZero() || p.To.Sub(p.From) > 31*24*time.Hour) {
		return true
	}
	// Без выбора точек отчёт берёт все доступные: у сотрудника магазина — свои, у руководства — всю сеть.
	points := len(p.Locations)
	if points == 0 {
		if sess, ok := auth.FromContext(ctx); ok && len(sess.LocationIDs) > 0 {
			points = len(sess.LocationIDs)
		} else {
			_ = s.pool.QueryRow(ctx, `select count(*) from locations where is_active`).Scan(&points)
		}
	}
	return points > 50
}

// plain приводит значение базы к числу, строке или дате — для JSON и Excel.
func plain(v any) any {
	switch x := v.(type) {
	case pgtype.Numeric:
		f, _ := x.Float64Value()
		return f.Float64
	case int32:
		return int64(x)
	case int16:
		return int64(x)
	case time.Time:
		return x.Format("2006-01-02")
	case [16]byte:
		return uuid.UUID(x).String()
	}
	return v
}

func format(c Column, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		switch c.Kind {
		case Money:
			return money(x)
		case Liters:
			return strings.Replace(fmt.Sprintf("%.3f", x), ".", ",", 1)
		case Percent:
			return strings.Replace(fmt.Sprintf("%.1f %%", x), ".", ",", 1)
		}
		return strings.Replace(fmt.Sprintf("%g", x), ".", ",", 1)
	case int64:
		return fmt.Sprint(x)
	case string:
		if c.Kind == Date && len(x) == 10 {
			return x[8:10] + "." + x[5:7] + "." + x[0:4]
		}
		return x
	}
	return fmt.Sprint(v)
}

func money(f float64) string {
	k := int64(f*100 + 0.5)
	if f < 0 {
		k = int64(f*100 - 0.5)
	}
	sign := ""
	if k < 0 {
		sign, k = "-", -k
	}
	whole := fmt.Sprint(k / 100)
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s%s,%02d", sign, b.String(), k%100)
}

func subtitle(d Def, p Params) string {
	var parts []string
	if slices.Contains(d.Params, "period") {
		parts = append(parts, fmt.Sprintf("Период: %s – %s", p.From.Format("02.01.2006"), p.To.Format("02.01.2006")))
	}
	if slices.Contains(d.Params, "date") {
		parts = append(parts, "На дату "+p.Date.Format("02.01.2006"))
	}
	if slices.Contains(d.Params, "days") {
		parts = append(parts, fmt.Sprintf("Без продаж %d дней", p.Days))
	}
	return strings.Join(parts, ". ")
}

// Render — файл отчёта: xlsx или pdf.
func (s *Service) Render(ctx context.Context, code string, p Params, kind string) ([]byte, string, error) {
	d, t, err := s.Run(ctx, code, p)
	if err != nil {
		return nil, "", err
	}
	_ = p.normalize(d)
	var org string
	_ = s.pool.QueryRow(ctx, `select coalesce((select value from app_settings where key = 'organization_name'), 'ООО «Чекушка»')`).Scan(&org)
	sess, _ := auth.FromContext(ctx)
	var author string
	_ = s.pool.QueryRow(ctx, `select full_name from users where id = $1`, sess.UserID).Scan(&author)
	h := forms.TableHeader{Organization: org, Title: d.Title, Subtitle: subtitle(d, p), Author: author, Created: time.Now()}
	cols := make([]forms.TableColumn, len(d.Columns))
	for i, c := range d.Columns {
		cols[i] = forms.TableColumn{Title: c.Title, Numeric: c.Kind != Text && c.Kind != Date, Sum: c.Sum}
	}
	if kind == "xlsx" {
		data, err := forms.TableXLSX(h, cols, t.Rows)
		return data, d.Title, err
	}
	rows := make([][]string, len(t.Rows))
	sums := make([]float64, len(cols))
	hasSum := false
	for i, r := range t.Rows {
		rows[i] = make([]string, len(r))
		for j, v := range r {
			rows[i][j] = format(d.Columns[j], v)
			if d.Columns[j].Sum {
				hasSum = true
				switch x := v.(type) {
				case float64:
					sums[j] += x
				case int64:
					sums[j] += float64(x)
				}
			}
		}
	}
	var totals []string
	if hasSum {
		totals = make([]string, len(cols))
		totals[0] = "Итого"
		for j, c := range d.Columns {
			if c.Sum {
				if c.Kind == Int {
					totals[j] = fmt.Sprint(int64(sums[j]))
				} else {
					totals[j] = format(c, sums[j])
				}
			}
		}
	}
	data, err := forms.TablePDF(h, cols, rows, totals)
	return data, d.Title, err
}

// File — отчёт в «Моих отчётах».
type File struct {
	ID                            uuid.UUID
	Report, Title, Format, Status string
	Error                         *string
	CreatedAt                     time.Time
	FinishedAt                    *time.Time
	ExpiresAt                     time.Time
}

// GenerateArgs — фоновая задача: сформировать файл отчёта.
type GenerateArgs struct {
	FileID uuid.UUID `json:"file_id"`
}

func (GenerateArgs) Kind() string { return "report" }

// Enqueue ставит тяжёлый отчёт в очередь фоновых задач; о готовности придёт уведомление.
func (s *Service) Enqueue(ctx context.Context, code string, p Params, kind string) (File, error) {
	d, ok := find(code)
	if !ok || !allowed(ctx, d) {
		return File{}, ErrNotFound
	}
	if err := p.normalize(d); err != nil {
		return File{}, err
	}
	if _, err := s.args(ctx, d, p); err != nil {
		return File{}, err
	}
	raw, _ := json.Marshal(p)
	sess, _ := auth.FromContext(ctx)
	var f File
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into report_files (user_id, report, title, params, format) values ($1, $2, $3, $4, $5)
			returning id, report, title, format, status, created_at, expires_at`, sess.UserID, code, d.Title+". "+subtitle(d, p), raw, kind).
			Scan(&f.ID, &f.Report, &f.Title, &f.Format, &f.Status, &f.CreatedAt, &f.ExpiresAt)
		if err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, tx, GenerateArgs{FileID: f.ID}, nil)
		return err
	})
	return f, err
}

// Generate — тело фоновой задачи: отчёт строится с правами того, кто его заказал.
func (s *Service) Generate(ctx context.Context, id uuid.UUID) error {
	var user uuid.UUID
	var code, kind, title string
	var raw []byte
	err := s.pool.QueryRow(ctx, `update report_files set status = 'running' where id = $1 and status in ('queued', 'running')
		returning user_id, report, format, params, title`, id).Scan(&user, &code, &kind, &raw, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var p Params
	_ = json.Unmarshal(raw, &p)
	sess := auth.Session{UserID: user}
	if err := s.pool.QueryRow(ctx, `select u.role, coalesce(array_agg(ul.location_id) filter (where ul.location_id is not null), '{}')
		from users u left join user_locations ul on ul.user_id = u.id where u.id = $1 and not u.is_blocked group by u.id`, user).
		Scan(&sess.Role, &sess.LocationIDs); err != nil {
		return s.fail(ctx, id, "Пользователь заблокирован или удалён")
	}
	data, _, err := s.Render(auth.WithSession(ctx, sess), code, p, kind)
	if err != nil {
		return s.fail(ctx, id, err.Error())
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update report_files set status = 'done', data = $2, finished_at = now() where id = $1`, id, data); err != nil {
			return err
		}
		return notify.Send(ctx, tx, user, "report_ready", "report:"+id.String(), "Отчёт готов", title, "/reports/files")
	})
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, msg string) error {
	_, err := s.pool.Exec(ctx, `update report_files set status = 'failed', error = $2, finished_at = now() where id = $1`, id, msg)
	return err
}

func (s *Service) Files(ctx context.Context) ([]File, error) {
	sess, _ := auth.FromContext(ctx)
	rows, err := s.pool.Query(ctx, `select id, report, title, format, status, error, created_at, finished_at, expires_at
		from report_files where user_id = $1 and expires_at > now() order by created_at desc`, sess.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (File, error) {
		var f File
		return f, r.Scan(&f.ID, &f.Report, &f.Title, &f.Format, &f.Status, &f.Error, &f.CreatedAt, &f.FinishedAt, &f.ExpiresAt)
	})
}

func (s *Service) Download(ctx context.Context, id uuid.UUID) ([]byte, string, error) {
	sess, _ := auth.FromContext(ctx)
	var data []byte
	var kind, status string
	err := s.pool.QueryRow(ctx, `select data, format, status from report_files where id = $1 and user_id = $2 and expires_at > now()`,
		id, sess.UserID).Scan(&data, &kind, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if status != "done" {
		return nil, "", ErrNotReady
	}
	return data, kind, nil
}

// Cleanup удаляет файлы старше семи дней.
func (s *Service) Cleanup(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `delete from report_files where expires_at < now()`)
	return err
}

// Worker выполняет задачу GenerateArgs.
type Worker struct {
	river.WorkerDefaults[GenerateArgs]
	Svc *Service
}

func (w *Worker) Work(ctx context.Context, job *river.Job[GenerateArgs]) error {
	return w.Svc.Generate(ctx, job.Args.FileID)
}
