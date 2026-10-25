// Command pikni-admin — служебные действия из консоли сервера: учётные записи и загрузка каталога.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/demo"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/seed"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

const usage = `usage:
  pikni-admin create-admin -login <login> [-name <full name>]
  pikni-admin reset-password -login <login>
  pikni-admin import-catalog -file <catalog.xlsx|catalog.csv> [-if-empty]
  pikni-admin seed-demo-year [-days 365] [-small МГ-02] [-rests rests.json]
  pikni-admin gen-catalog -file <catalog.csv>
  pikni-admin seed-users
  pikni-admin scenario <name|all|list>
  pikni-admin seed-federal [-stores 3000] [-small-share 0.1]
  pikni-admin seed-load [-stocktakes 500] [-scanners 10000] -file <plan.json>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pikni-admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd := args[0]
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	login := flags.String("login", "", "user login")
	name := flags.String("name", "Администратор", "full name for create-admin")
	file := flags.String("file", "", "catalog file for import-catalog")
	days := flags.Int("days", 365, "seed-demo-year: how many days of history")
	small := flags.String("small", "МГ-02", "seed-demo-year: comma-separated codes of neighbourhood stores")
	rests := flags.String("rests", "", "seed-demo-year: write EGAIS rests for govsim seed-rests to this file")
	stores := flags.Int("stores", 3000, "seed-federal: stores in the federal network")
	smallShare := flags.Float64("small-share", 0.1, "seed-federal: share of neighbourhood stores")
	stocktakes := flags.Int("stocktakes", 500, "seed-load: stores counted at once")
	scanners := flags.Int("scanners", 10000, "seed-load: storekeepers scanning at once")
	ifEmpty := flags.Bool("if-empty", false, "import-catalog: skip if the catalog already has products")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	// Каталог генерируется без базы: результат — файл для import-catalog.
	if cmd == "gen-catalog" {
		return genCatalog(*file)
	}
	if cmd == "scenario" && (flags.Arg(0) == "" || flags.Arg(0) == "list") {
		for _, sc := range seed.Scenarios {
			fmt.Printf("%-22s %s (вход: %s)\n", sc.Name, sc.Title, sc.Login)
		}
		return nil
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch cmd {
	case "create-admin", "reset-password":
		if *login == "" {
			return errors.New("-login is required\n" + usage)
		}
		return account(ctx, pool, cmd, *login, *name)
	case "seed-federal":
		owner, err := db.NewPool(ctx, cfg.DatabaseMigratorURL)
		if err != nil {
			return err
		}
		defer owner.Close()
		return demo.Federal(ctx, owner, demo.FederalOptions{Stores: *stores, SmallShare: *smallShare}, slog.Default())
	case "seed-load":
		if *file == "" {
			return errors.New("seed-load: -file is required")
		}
		f, err := os.Create(*file)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		return seed.Load(ctx, pool, *stocktakes, *scanners, f, slog.Default())
	case "scenario":
		env := seed.NewEnv(pool, cfg.EgaisURL, envOr("GOVSIM_ADMIN_PASSWORD", "govsim"), slog.Default())
		return env.Run(ctx, flags.Arg(0))
	case "seed-users":
		accounts, err := seed.Users(ctx, pool)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			fmt.Printf("%-14s %-14s %s\n", a.Login, a.Role, a.Location)
		}
		fmt.Printf("Password: %s, cashier PIN: %s\n", seed.Password, seed.PIN)
		return nil
	case "seed-demo-year":
		return seedDemoYear(ctx, pool, *days, *small, *rests)
	case "import-catalog":
		if *file == "" {
			return errors.New("-file is required\n" + usage)
		}
		return importCatalog(ctx, pool, *file, *ifEmpty)
	}
	return errors.New(usage)
}

func account(ctx context.Context, pool *pgxpool.Pool, cmd, login, name string) error {
	svc := users.NewService(pool)
	var temporary string
	var err error
	if cmd == "create-admin" {
		temporary, err = svc.CreateAdmin(ctx, login, name)
	} else {
		temporary, err = svc.ResetPasswordByLogin(ctx, login)
	}
	if err != nil {
		return err
	}
	fmt.Printf("Temporary password for %s: %s\nThe user will be asked to change it at first sign-in.\n", login, temporary)
	return nil
}

// importCatalog загружает каталог тем же импортом, что и экран «Товары»: проверки одинаковые.
func importCatalog(ctx context.Context, pool *pgxpool.Pool, file string, ifEmpty bool) error {
	if ifEmpty {
		var products int
		if err := pool.QueryRow(ctx, "select count(*) from products").Scan(&products); err != nil {
			return err
		}
		if products > 0 {
			fmt.Printf("Catalog already has %d products, import skipped.\n", products)
			return nil
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	report, err := catalog.NewService(pool).Import(ctx, filepath.Base(file), data, true, nil, nil)
	if err != nil {
		return err
	}
	for _, e := range report.Errors {
		fmt.Fprintf(os.Stderr, "row %d: %s\n", e.Row, e.Message)
	}
	if !report.Committed {
		return fmt.Errorf("catalog not imported: %d rows with errors", len(report.Errors))
	}
	fmt.Printf("Imported %d products with %d variants.\n", report.Products, report.Variants)
	return nil
}

func genCatalog(file string) error {
	if file == "" {
		return errors.New("gen-catalog: -file is required")
	}
	items, err := seed.Catalog()
	if err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	if err := seed.WriteCSV(f, items); err != nil {
		_ = f.Close()
		return err
	}
	fmt.Printf("Catalog: %d variants written to %s.\n", len(items), file)
	return f.Close()
}

func seedDemoYear(ctx context.Context, pool *pgxpool.Pool, days int, small, rests string) error {
	opts := demo.Options{Days: days, Small: strings.Split(small, ",")}
	if rests != "" {
		f, err := os.Create(rests)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		opts.Rests = f
	}
	return demo.Generate(ctx, pool, opts, slog.Default())
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
