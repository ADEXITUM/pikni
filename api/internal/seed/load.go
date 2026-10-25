package seed

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/marks"
)

// LoadPlan — сотрудники федеральной ревизии для нагрузочного испытания: у каждого сеанс, ревизия,
// зона и марки, которые он отсканирует. Читает cmd/loadtest -plan.
type LoadPlan struct {
	Scanners []Scanner `json:"scanners"`
}

type Scanner struct {
	Token string   `json:"token"`
	CSRF  string   `json:"csrf"`
	Doc   string   `json:"doc"`
	Zone  string   `json:"zone"`
	Codes []string `json:"codes"`
}

// Load начинает полные ревизии в stocktakes крупнейших магазинах федеральной сети и заводит
// scanners кладовщиков поровну на зоны склада и зала. Сеансы создаются сразу в базе: вход через
// API ограничен частотой, и десять тысяч человек с одного адреса не вошли бы.
func Load(ctx context.Context, pool *pgxpool.Pool, stocktakes, scanners int, w io.Writer, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `select l.id, l.code,
			(select id from zones where location_id = l.id and kind = 'backroom'),
			(select id from zones where location_id = l.id and kind = 'floor')
		from locations l where l.code like 'ФМ-%'
		order by (select count(*) from assortment_matrix m where m.location_id = l.id) desc, l.code limit $1`, stocktakes)
	if err != nil {
		return err
	}
	type store struct {
		ID          uuid.UUID
		Code        string
		Back, Floor uuid.UUID
	}
	stores, err := pgx.CollectRows(rows, pgx.RowToStructByPos[store])
	if err != nil {
		return err
	}
	if len(stores) == 0 {
		return fmt.Errorf("no federal stores: run pikni-admin seed-federal first")
	}
	docs := documents.NewService(pool, catalog.NewService(pool), marks.NewService(pool))
	perStore := max(2, scanners/len(stores))
	var plan LoadPlan
	var users, links, sessions [][]any
	began := time.Now()
	for k, s := range stores {
		var manager uuid.UUID
		if err := pool.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, must_change_password)
			values ($1, $2, 'store_manager', 'x', false) returning id`, "load-upr-"+s.Code, "Управляющий "+s.Code).Scan(&manager); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2)`, manager, s.ID); err != nil {
			return err
		}
		mctx := auth.WithSession(ctx, auth.Session{UserID: manager, Role: "store_manager", LocationIDs: []uuid.UUID{s.ID}, TOTPEnabled: true})
		doc, err := docs.CreateStocktake(mctx, s.ID, documents.Scope{}, "Федеральная ревизия — нагрузочное испытание")
		if err != nil {
			return err
		}
		var version int32
		if err := pool.QueryRow(ctx, `select version from documents where id = $1`, doc).Scan(&version); err != nil {
			return err
		}
		if err := docs.StartStocktake(mctx, doc, version); err != nil {
			return err
		}
		zoneCodes := map[uuid.UUID][]string{}
		for _, z := range []uuid.UUID{s.Back, s.Floor} {
			rows, err := pool.Query(ctx, `select code from excise_marks where zone_id = $1 and status = 'in_stock' order by code`, z)
			if err != nil {
				return err
			}
			if zoneCodes[z], err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
				return err
			}
		}
		for j := range perStore {
			zone := s.Back
			if j%2 == 1 {
				zone = s.Floor
			}
			id := uuid.Must(uuid.NewV7())
			token, csrf := rand.Text(), rand.Text()
			hash := sha256.Sum256([]byte(token))
			users = append(users, []any{id, fmt.Sprintf("load-%s-%d", s.Code, j+1), "Кладовщик " + s.Code, "storekeeper", "x", false})
			links = append(links, []any{id, s.ID})
			sessions = append(sessions, []any{id, hash[:], csrf, time.Now().Add(24 * time.Hour)})
			// Каждый сканирует свою часть зоны: марки делятся по кругу между сотрудниками зоны.
			var codes []string
			all := zoneCodes[zone]
			for c := j / 2; c < len(all) && len(codes) < 300; c += (perStore + 1) / 2 {
				codes = append(codes, all[c])
			}
			plan.Scanners = append(plan.Scanners, Scanner{Token: token, CSRF: csrf, Doc: doc.String(), Zone: zone.String(), Codes: codes})
		}
		if (k+1)%50 == 0 {
			log.Info("load plan", "stocktakes", k+1, "of", len(stores), "elapsed", time.Since(began).Round(time.Second))
		}
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"users"}, []string{"id", "login", "full_name", "role", "password_hash", "must_change_password"}, pgx.CopyFromRows(users)); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"user_locations"}, []string{"user_id", "location_id"}, pgx.CopyFromRows(links)); err != nil {
			return err
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{"sessions"}, []string{"user_id", "token_hash", "csrf_token", "expires_at"}, pgx.CopyFromRows(sessions))
		return err
	})
	if err != nil {
		return err
	}
	log.Info("load plan ready", "stocktakes", len(stores), "scanners", len(plan.Scanners))
	return json.NewEncoder(w).Encode(plan)
}
