package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit/store"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

type Handler struct {
	q *store.Queries
}

func NewHandler(db DB) *Handler {
	return &Handler{q: store.New(db)}
}

func (h *Handler) ListAuditLog(ctx context.Context, req apispec.ListAuditLogRequestObject) (apispec.ListAuditLogResponseObject, error) {
	p := req.Params
	limit := defaultPageSize
	if p.Limit != nil {
		limit = min(max(*p.Limit, 1), maxPageSize)
	}

	// Одна лишняя строка показывает, есть ли следующая страница.
	rows, err := h.q.ListEntries(ctx, store.ListEntriesParams{
		Before:     p.Before,
		FromAt:     p.From,
		ToAt:       p.To,
		UserID:     p.UserID,
		Action:     (*string)(p.Action),
		EntityType: p.EntityType,
		EntityID:   p.EntityID,
		MaxRows:    int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}

	page := apispec.AuditPage{Items: make([]apispec.AuditEntry, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit {
			page.NextBefore = &page.Items[limit-1].ID
			break
		}
		entry, err := toAPI(r)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, entry)
	}
	return apispec.ListAuditLog200JSONResponse(page), nil
}

func toAPI(r store.ListEntriesRow) (apispec.AuditEntry, error) {
	e := apispec.AuditEntry{
		ID:         r.ID,
		At:         r.At,
		Action:     apispec.AuditAction(r.Action),
		SessionID:  r.SessionID,
		EntityType: r.EntityType,
		EntityID:   r.EntityID,
	}
	if err := json.Unmarshal(r.Data, &e.Data); err != nil {
		return e, fmt.Errorf("decode audit data %d: %w", r.ID, err)
	}
	if r.UserID != nil && r.UserLogin != nil && r.UserFullName != nil {
		e.User = &apispec.AuditUser{ID: *r.UserID, Login: *r.UserLogin, FullName: *r.UserFullName}
	}
	if r.IP != nil {
		ip := r.IP.String()
		e.IP = &ip
	}
	return e, nil
}
