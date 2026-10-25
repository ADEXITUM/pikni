package assets_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/assets"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/forms"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

type fixture struct {
	svc                           *assets.Service
	blg, khv                      uuid.UUID
	accountant, manager, keeper   context.Context
	accountantID, keeperID, mgrID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{svc: assets.NewService(db.App)}
	store := func(code string) uuid.UUID {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code)
			values ($1, 'Магазин ' || $1, 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`, code).Scan(&id))
		return id
	}
	f.blg, f.khv = store("МГ-01"), store("МГ-03")
	user := func(login, role string, location *uuid.UUID) (uuid.UUID, context.Context) {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ($1, $1, $2, 'x') returning id`,
			login, role).Scan(&id))
		s := auth.Session{UserID: id, Role: role}
		if location != nil {
			_, err := db.App.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2)`, id, *location)
			must(err)
			s.LocationIDs = []uuid.UUID{*location}
		}
		return id, auth.WithSession(ctx, s)
	}
	f.accountantID, f.accountant = user("buh", "accountant", nil)
	f.mgrID, f.manager = user("boss", "manager", nil)
	f.keeperID, f.keeper = user("keeper", "storekeeper", &f.blg)
	return f
}

func (f *fixture) version(t *testing.T, id uuid.UUID) int32 {
	t.Helper()
	c, err := f.svc.Get(f.accountant, id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Asset.Version
}

func TestAssetLifecycle(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	fridge := assets.Fields{Name: "Холодильный шкаф Polair", Category: "refrigeration", Serial: "PL-778",
		AcceptedAt: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC), CostKopeck: 8_950_000}
	if _, err := f.svc.Create(f.keeper, f.blg, nil, fridge); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("keeper creates asset: %v", err)
	}
	id, err := f.svc.Create(f.accountant, f.blg, &f.keeperID, fridge)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := f.svc.Get(f.keeper, id)
	if c.Asset.Number != "INV-000001" || c.Asset.ResponsibleName == nil || *c.Asset.ResponsibleName != "keeper" {
		t.Fatalf("created: %+v", c.Asset)
	}

	// Перемещение в другую точку со сменой МОЛ — две записи в истории.
	if err := f.svc.Move(f.accountant, id, f.version(t, id), f.khv, &f.accountantID, "Открытие МГ-03"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Move(f.accountant, id, c.Asset.Version, f.khv, nil, ""); !errors.Is(err, assets.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if _, err := f.svc.Get(f.keeper, id); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("keeper sees asset of another store: %v", err)
	}
	if err := f.svc.SetStatus(f.accountant, id, f.version(t, id), assets.StatusRepair, "Не холодит"); err != nil {
		t.Fatal(err)
	}
	c, _ = f.svc.Get(f.accountant, id)
	kinds := []string{}
	for _, e := range c.Events {
		kinds = append(kinds, e.Kind)
	}
	if len(kinds) != 4 || kinds[0] != "status" || kinds[1] != "responsible" || kinds[2] != "moved" || kinds[3] != "created" {
		t.Fatalf("history: %v", kinds)
	}

	// Списание «двумя ключами»: оформил бухгалтер, утверждает руководитель.
	if err := f.svc.RequestWriteoff(f.accountant, id, f.version(t, id), "Ремонт нецелесообразен"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RequestWriteoff(f.accountant, id, f.version(t, id), "ещё раз"); !errors.Is(err, assets.ErrPendingWriteoff) {
		t.Fatalf("second request: %v", err)
	}
	if err := f.svc.DecideWriteoff(f.accountant, id, true, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("accountant approves: %v", err)
	}
	if err := f.svc.DecideWriteoff(f.manager, id, true, "Согласен"); err != nil {
		t.Fatal(err)
	}
	c, _ = f.svc.Get(f.accountant, id)
	if c.Asset.Status != assets.StatusWrittenOff || c.Last == nil || c.Last.Status != "approved" {
		t.Fatalf("after approval: %s %+v", c.Asset.Status, c.Last)
	}
	if err := f.svc.Move(f.accountant, id, c.Asset.Version, f.blg, nil, ""); !errors.Is(err, assets.ErrWrittenOff) {
		t.Fatalf("move written-off: %v", err)
	}
	act, err := f.svc.WriteoffAct(f.accountant, id)
	if err != nil || !bytes.HasPrefix(act, []byte("%PDF")) {
		t.Fatalf("act: %v", err)
	}
	rows, _ := f.svc.List(f.accountant, assets.Filter{})
	if len(rows) != 0 {
		t.Fatalf("written-off in register by default: %d", len(rows))
	}
	written := assets.StatusWrittenOff
	rows, _ = f.svc.List(f.accountant, assets.Filter{Status: &written})
	if len(rows) != 1 {
		t.Fatalf("written-off filter: %d", len(rows))
	}
}

func TestAssetAudit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	add := func(name string, location uuid.UUID) uuid.UUID {
		id, err := f.svc.Create(f.accountant, location, nil, assets.Fields{Name: name, Category: "furniture",
			AcceptedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), CostKopeck: 100_000})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	table, chair, rack := add("Стол", f.blg), add("Стул", f.blg), add("Стеллаж", f.khv)

	audit, err := f.svc.StartAudit(f.accountant, f.blg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartAudit(f.accountant, f.blg); !errors.Is(err, assets.ErrAuditOpen) {
		t.Fatalf("second audit: %v", err)
	}
	if r, err := f.svc.AuditScan(f.accountant, audit, " inv-000001 "); err != nil || r.AssetID != table || r.Elsewhere {
		t.Fatalf("scan table: %+v %v", r, err)
	}
	if r, _ := f.svc.AuditScan(f.accountant, audit, "INV-000001"); !r.Duplicate {
		t.Fatalf("repeat scan: %+v", r)
	}
	if r, err := f.svc.AuditScan(f.accountant, audit, "INV-000003"); err != nil || r.AssetID != rack || !r.Elsewhere {
		t.Fatalf("scan rack from another store: %+v %v", r, err)
	}
	if _, err := f.svc.AuditScan(f.accountant, audit, "4600000000016"); !errors.Is(err, assets.ErrNotAsset) {
		t.Fatalf("barcode: %v", err)
	}
	st, _ := f.svc.Audit(f.accountant, audit)
	got := map[uuid.UUID]string{}
	for _, r := range st.Results {
		switch {
		case r.Found && r.Elsewhere:
			got[r.ID] = "elsewhere"
		case r.Found:
			got[r.ID] = "found"
		default:
			got[r.ID] = "not_found"
		}
	}
	if got[table] != "found" || got[chair] != "not_found" || got[rack] != "elsewhere" || len(got) != 3 {
		t.Fatalf("results: %v", got)
	}
	if err := f.svc.FinishAudit(f.accountant, audit, true, false); err != nil {
		t.Fatal(err)
	}
	c, _ := f.svc.Get(f.accountant, rack)
	if c.Asset.LocationID != f.blg || c.Events[0].Kind != "found_elsewhere" {
		t.Fatalf("rack relocated: %s %s", c.Asset.LocationName, c.Events[0].Kind)
	}
	if _, err := f.svc.AuditScan(f.accountant, audit, "INV-000002"); !errors.Is(err, assets.ErrAuditClosed) {
		t.Fatalf("scan after finish: %v", err)
	}
	labels, err := f.svc.Labels(f.accountant, []uuid.UUID{table, chair}, forms.A4)
	if err != nil || !bytes.HasPrefix(labels, []byte("%PDF")) {
		t.Fatalf("labels: %v", err)
	}
	if xlsx, err := f.svc.Register(f.accountant, assets.Filter{}); err != nil || len(xlsx) < 1000 {
		t.Fatalf("register: %d %v", len(xlsx), err)
	}
}
