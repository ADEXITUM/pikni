package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func IsUniqueViolation(err error) bool {
	return hasCode(err, "23505")
}

// IsUniqueViolationOn различает, какое из уникальных ограничений таблицы нарушено.
func IsUniqueViolationOn(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func IsForeignKeyViolation(err error) bool {
	return hasCode(err, "23503")
}

func IsForeignKeyViolationOn(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
