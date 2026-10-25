package assets

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/assets/store"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

// Инвентаризация имущества (ИМ-4): точку обходят со смартфоном и сканируют наклейки. Результат
// по каждому объекту — найден, не найден или найден здесь, хотя числится в другой точке.

func (s *Service) StartAudit(ctx context.Context, location uuid.UUID) (uuid.UUID, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, location); err != nil {
		return uuid.Nil, err
	}
	id, err := s.q.CreateAudit(ctx, store.CreateAuditParams{LocationID: location, StartedBy: me(ctx)})
	if db.IsUniqueViolation(err) {
		return uuid.Nil, ErrAuditOpen
	}
	return id, err
}

func (s *Service) Audits(ctx context.Context) ([]store.ListAuditsRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionAssetsView)
	if err != nil {
		return nil, err
	}
	if allowed != nil && len(allowed) == 0 {
		return nil, nil
	}
	return s.q.ListAudits(ctx, allowed)
}

type AuditState struct {
	Audit   store.GetAuditRow
	Results []store.AuditResultsRow
}

func (s *Service) Audit(ctx context.Context, id uuid.UUID) (AuditState, error) {
	var st AuditState
	a, err := s.q.GetAudit(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, err
	}
	if auth.CheckLocation(ctx, apispec.PermissionAssetsView, a.LocationID) != nil {
		return st, ErrNotFound
	}
	st.Audit = a
	st.Results, err = s.q.AuditResults(ctx, id)
	return st, err
}

func (s *Service) lockAudit(ctx context.Context, q *store.Queries, id uuid.UUID) (store.LockAuditRow, error) {
	a, err := q.LockAudit(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, a.LocationID); err != nil {
		return a, err
	}
	if a.Status != "counting" {
		return a, ErrAuditClosed
	}
	return a, nil
}

// ScanResult — исход скана наклейки.
type ScanResult struct {
	AssetID   uuid.UUID
	Elsewhere bool
	Duplicate bool
}

func (s *Service) AuditScan(ctx context.Context, id uuid.UUID, raw string) (ScanResult, error) {
	var res ScanResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		au, err := s.lockAudit(ctx, q, id)
		if err != nil {
			return err
		}
		a, err := q.AssetByNumber(ctx, raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAsset
		}
		if err != nil {
			return err
		}
		n, err := q.AddAuditScan(ctx, store.AddAuditScanParams{AuditID: id, AssetID: a.ID, ScannedBy: me(ctx)})
		if err != nil {
			return err
		}
		res = ScanResult{AssetID: a.ID, Elsewhere: a.LocationID != au.LocationID, Duplicate: n == 0}
		return nil
	})
	return res, err
}

// FinishAudit завершает инвентаризацию. relocate — найденное здесь, но числящееся в другой точке,
// переводится в эту точку с записью в истории объекта.
func (s *Service) FinishAudit(ctx context.Context, id uuid.UUID, relocate, cancel bool) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		au, err := s.lockAudit(ctx, q, id)
		if err != nil {
			return err
		}
		status := "finished"
		if cancel {
			status = "cancelled"
		}
		if relocate && !cancel {
			rows, err := q.AuditResults(ctx, id)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if !r.Found || !r.Elsewhere {
					continue
				}
				a, err := q.LockAsset(ctx, r.ID)
				if err != nil {
					return err
				}
				if a.Status == StatusWrittenOff {
					continue
				}
				if err := q.SetAssetLocation(ctx, store.SetAssetLocationParams{ID: r.ID, LocationID: au.LocationID}); err != nil {
					return err
				}
				if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
					AssetID: r.ID, Kind: "found_elsewhere", FromLocationID: &a.LocationID, ToLocationID: &au.LocationID,
					Comment: "Найден при инвентаризации имущества", UserID: ptr(me(ctx)),
				}); err != nil {
					return err
				}
			}
		}
		if err := q.FinishAudit(ctx, store.FinishAuditParams{ID: id, Status: status, FinishedBy: ptr(me(ctx))}); err != nil {
			return err
		}
		if cancel {
			return nil
		}
		return record(ctx, tx, apispec.AuditActionAssetAuditFinished, id, map[string]any{"relocate": relocate})
	})
}
