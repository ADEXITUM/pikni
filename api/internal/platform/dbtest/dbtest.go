// Package dbtest поднимает PostgreSQL в контейнере для интеграционных тестов.
//
// Контейнер запускается один раз на пакет тестов, миграции применяются к шаблонной базе,
// а каждый тест получает свою копию шаблона. Контейнер удаляет Ryuk из testcontainers,
// когда процесс тестов завершится.
package dbtest

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ADEXITUM/pikni/api/db/initdb"
	"github.com/ADEXITUM/pikni/api/db/migrations"
)

const (
	image    = "postgres:18"
	template = "pikni"
)

type DB struct {
	Owner *pgxpool.Pool
	App   *pgxpool.Pool
}

var (
	startOnce sync.Once
	startErr  error
	baseURL   *url.URL
	admin     *pgxpool.Pool
	seq       atomic.Int64
)

func New(t *testing.T) *DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Docker; skipped in -short mode")
	}

	startOnce.Do(func() { startErr = start() })
	if startErr != nil {
		t.Fatalf("start postgres: %v", startErr)
	}

	name := fmt.Sprintf("test_%d", seq.Add(1))
	if _, err := admin.Exec(t.Context(), "create database "+name+" template "+template+" owner pikni_owner"); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	return &DB{
		Owner: connect(t, "pikni_owner", name),
		App:   connect(t, "pikni_app", name),
	}
}

func WantCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("error = %v, want SQLSTATE %s", err, code)
	}
}

func start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ctr, err := postgres.Run(ctx, image,
		postgres.WithDatabase(template),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithEnv(map[string]string{
			"PIKNI_OWNER_PASSWORD": "pikni_owner",
			"PIKNI_APP_PASSWORD":   "pikni_app",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            bytes.NewReader(initdb.RolesScript),
			ContainerFilePath: "/docker-entrypoint-initdb.d/10-roles.sh",
			FileMode:          0o755,
		}),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return err
	}

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return err
	}
	if baseURL, err = url.Parse(dsn); err != nil {
		return err
	}

	if err := migrate(ctx, dsnFor("pikni_owner", template)); err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}

	admin, err = pgxpool.New(ctx, dsnFor("postgres", "postgres"))
	return err
}

// Шаблон нельзя копировать, пока к нему кто-то подключён, поэтому соединение мигратора закрывается сразу.
func migrate(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func connect(t *testing.T, user, database string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsnFor(user, database))
	if err != nil {
		t.Fatalf("connect as %s: %v", user, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// В тестовом кластере пароль каждой роли совпадает с её именем.
func dsnFor(user, database string) string {
	u := *baseURL
	u.User = url.UserPassword(user, user)
	u.Path = "/" + database
	return u.String()
}
