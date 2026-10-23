// Package initdb хранит скрипт первичной настройки кластера PostgreSQL: роли и права.
package initdb

import _ "embed"

//go:embed 10-roles.sh
var RolesScript []byte
