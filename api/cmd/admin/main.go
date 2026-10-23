// Command pikni-admin — служебные действия из консоли сервера: учётные записи и загрузка каталога.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

const usage = `usage:
  pikni-admin create-admin -login <login> [-name <full name>]
  pikni-admin reset-password -login <login>
  pikni-admin import-catalog -file <catalog.xlsx|catalog.csv> [-if-empty]`

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
	ifEmpty := flags.Bool("if-empty", false, "import-catalog: skip if the catalog already has products")
	if err := flags.Parse(args[1:]); err != nil {
		return err
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
	report, err := catalog.NewService(pool).Import(ctx, filepath.Base(file), data, true, nil)
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
