package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func IsUniqueViolation(err error) bool {
	return hasCode(err, "23505")
}

func IsForeignKeyViolation(err error) bool {
	return hasCode(err, "23503")
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
