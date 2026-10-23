// Command pikni-admin — служебные действия с учётными записями из консоли сервера.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"

	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

const usage = `usage:
  pikni-admin create-admin -login <login> [-name <full name>]
  pikni-admin reset-password -login <login>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pikni-admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || !slices.Contains([]string{"create-admin", "reset-password"}, args[0]) {
		return errors.New(usage)
	}
	cmd := args[0]

	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	login := flags.String("login", "", "user login")
	name := flags.String("name", "Администратор", "full name for create-admin")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *login == "" {
		return errors.New("-login is required\n" + usage)
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
	svc := users.NewService(pool)

	var temporary string
	switch cmd {
	case "create-admin":
		temporary, err = svc.CreateAdmin(ctx, *login, *name)
	case "reset-password":
		temporary, err = svc.ResetPasswordByLogin(ctx, *login)
	}
	if err != nil {
		return err
	}

	fmt.Printf("Temporary password for %s: %s\nThe user will be asked to change it at first sign-in.\n", *login, temporary)
	return nil
}
