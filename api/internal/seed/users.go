package seed

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/auth/password"
)

// Пароль и PIN учебных учётных записей: стенд для разработки и демонстрации, не для работы.
const (
	Password = "Пикни-демо-2026"
	PIN      = "2468"
)

// Account — учётная запись для ручной проверки сценариев.
type Account struct {
	Login, Name, Role string
	Location          string // код точки; пусто — вся сеть
}

// Accounts — по сотруднику каждой роли: руководство сети, на каждый магазин — управляющий,
// кладовщик и кассир, на каждый склад — кладовщик. В первом магазине логины короткие:
// upr, keeper, kassir.
func Accounts(ctx context.Context, pool *pgxpool.Pool) ([]Account, error) {
	out := []Account{
		{Login: "admin", Name: "Синдилеев К. А.", Role: "admin"},
		{Login: "director", Name: "Директор Ковалёва Е. П.", Role: "manager"},
		{Login: "buh", Name: "Бухгалтер Миронова О. В.", Role: "accountant"},
	}
	rows, err := pool.Query(ctx, `select code, kind from locations where is_active and kind in ('store', 'warehouse') order by kind desc, code`)
	if err != nil {
		return nil, err
	}
	type loc struct{ Code, Kind string }
	locs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[loc])
	if err != nil {
		return nil, err
	}
	first := true
	for _, l := range locs {
		suffix := "-" + strings.ToLower(strings.NewReplacer("МГ-", "mg", "СК-", "sk").Replace(l.Code))
		if l.Kind == "warehouse" {
			out = append(out, Account{Login: "keeper" + suffix, Name: "Кладовщик склада " + l.Code, Role: "storekeeper", Location: l.Code})
			continue
		}
		if first {
			suffix, first = "", false
		}
		out = append(out,
			Account{Login: "upr" + suffix, Name: "Управляющий " + l.Code, Role: "store_manager", Location: l.Code},
			Account{Login: "keeper" + suffix, Name: "Кладовщик " + l.Code, Role: "storekeeper", Location: l.Code},
			Account{Login: "kassir" + suffix, Name: "Кассир " + l.Code, Role: "cashier", Location: l.Code},
		)
	}
	return out, nil
}

// Users заводит учётные записи Accounts с паролем Password (кассирам — и PIN) без смены при
// первом входе. Повторный запуск возвращает пароль и снимает блокировку.
func Users(ctx context.Context, pool *pgxpool.Pool) ([]Account, error) {
	accounts, err := Accounts(ctx, pool)
	if err != nil {
		return nil, err
	}
	hash, err := password.Hash(Password)
	if err != nil {
		return nil, err
	}
	pin, err := password.Hash(PIN)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, a := range accounts {
			var pinHash any
			if a.Role == "cashier" {
				pinHash = pin
			}
			var id string
			if err := tx.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, pin_hash, must_change_password)
				values ($1, $2, $3, $4, $5, false)
				on conflict ((lower(login))) do update set full_name = excluded.full_name, role = excluded.role,
					password_hash = excluded.password_hash, pin_hash = excluded.pin_hash, must_change_password = false,
					is_blocked = false, locked_until = null
				returning id`, a.Login, a.Name, a.Role, hash, pinHash).Scan(&id); err != nil {
				return fmt.Errorf("%s: %w", a.Login, err)
			}
			if a.Location == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `insert into user_locations (user_id, location_id)
				select $1, id from locations where code = $2 on conflict do nothing`, id, a.Location); err != nil {
				return err
			}
		}
		return nil
	})
	return accounts, err
}
