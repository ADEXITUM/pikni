// Package simtest поднимает PostgreSQL в контейнере для тестов имитатора: контейнер один на пакет,
// каждый тест получает свою базу со схемой и демо-участниками.
package simtest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ADEXITUM/pikni/govsim/internal/sim"
)

// Каталог для тестов: две водки с марками и пиво с кодами «Честного знака».
const Catalog = "Наименование;Вид продукции;Крепость, %;Честный знак;Производитель;Объём, л;Алкокод;Штрихкод;Закупочная цена, ₽;GTIN\n" +
	"Водка «Тайга»;200;40;нет;ООО «Тайга»;0,5;0310000000000000102;2900000000025;290;\n" +
	"Водка «Белый Амур»;200;40;нет;АО «АЛВЗ»;0,5;0310000000000000107;2900000000070;270;\n" +
	"Пиво «Амурское»;500;4,5;да;АО «Амурский пивзавод»;0,45;0310000000000000132;2900000000322;52;\n"

const (
	Vodka    = "0310000000000000102"
	Vodka2   = "0310000000000000107"
	Beer     = "0310000000000000132"
	Store    = "030000100011"
	Store2   = "030000100012"
	Supplier = "030000200001"
)

var (
	once  sync.Once
	dsn   string
	admin *pgxpool.Pool
	err   error
	seq   atomic.Int64
)

func New(t *testing.T) *sim.Sim {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Docker; skipped in -short mode")
	}
	once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ctr, e := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("govsim"), postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"), postgres.BasicWaitStrategies())
		if e != nil {
			err = e
			return
		}
		if dsn, err = ctr.ConnectionString(ctx, "sslmode=disable"); err != nil {
			return
		}
		admin, err = pgxpool.New(ctx, dsn)
	})
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	name := fmt.Sprintf("t%d", seq.Add(1))
	if _, err := admin.Exec(t.Context(), "create database "+name); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), strings.Replace(dsn, "/govsim?", "/"+name+"?", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := sim.Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	s := sim.New(pool)
	if _, err := s.Seed(t.Context(), strings.NewReader(Catalog)); err != nil {
		t.Fatal(err)
	}
	return s
}
