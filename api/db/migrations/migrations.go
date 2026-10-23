// Package migrations встраивает SQL-миграции схемы в бинарник pikni-migrate.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
