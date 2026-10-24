package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InTx выполняет fn в транзакции. При взаимоблокировке (40P01) или конфликте сериализации (40001)
// транзакция повторяется — всего до трёх попыток: так проведение документов переживает гонки.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	var err error
	for range 3 {
		err = pgx.BeginFunc(ctx, pool, fn)
		if !hasCode(err, "40P01") && !hasCode(err, "40001") {
			return err
		}
	}
	return err
}
