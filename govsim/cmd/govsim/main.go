// govsim — учебный имитатор смежных систем стенда «Пикни»: ЕГАИС с УТМ точек, ГИС МТ «Честный знак»
// и поставщик. Подкоманды: serve — HTTP, migrate — схема, seed — участники и продукция из каталога,
// seed-rests — остатки точек после генерации демо-года.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/govsim/internal/sim"
	"github.com/ADEXITUM/pikni/govsim/internal/web"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: govsim serve | migrate | seed -catalog FILE | seed-rests -file FILE")
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, os.Args[1], os.Args[2:]); err != nil {
		log.Error("govsim failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cmd string, args []string) error {
	pool, err := pgxpool.New(ctx, env("GOVSIM_DATABASE_URL", "postgres://govsim:govsim@localhost:5432/govsim?sslmode=disable"))
	if err != nil {
		return err
	}
	defer pool.Close()

	switch cmd {
	case "migrate":
		return sim.Migrate(ctx, pool)
	case "seed":
		fs := flag.NewFlagSet("seed", flag.ExitOnError)
		catalog := fs.String("catalog", "", "демо-каталог «Пикни» (api/db/seed/catalog.csv)")
		_ = fs.Parse(args)
		f, err := os.Open(*catalog)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		n, err := sim.New(pool).Seed(ctx, f)
		if err == nil {
			fmt.Printf("Seeded participants and %d products.\n", n)
		}
		return err
	case "seed-rests":
		fs := flag.NewFlagSet("seed-rests", flag.ExitOnError)
		file := fs.String("file", "", "остатки точек (pikni-admin seed-demo-year -rests)")
		_ = fs.Parse(args)
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		marks, rests, err := sim.New(pool).SeedRests(ctx, f)
		if err == nil {
			fmt.Printf("Balances: %d marks, %d quantity rests.\n", marks, rests)
		}
		return err
	case "serve":
		if err := sim.Migrate(ctx, pool); err != nil {
			return err
		}
		srv := &http.Server{
			Addr: env("GOVSIM_ADDR", "localhost:8090"),
			Handler: web.New(sim.New(pool), log, env("GOVSIM_ADMIN_PASSWORD", "govsim"),
				env("GOVSIM_PUBLIC_URL", "http://localhost:8090")).Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()
		log.Info("govsim listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}
