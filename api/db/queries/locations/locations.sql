-- name: ListLocations :many
select id, code, name, kind, city, timezone
from locations
order by code;
