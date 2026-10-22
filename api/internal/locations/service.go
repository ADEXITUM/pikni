// Package locations — точки сети: склады и магазины.
package locations

import (
	"context"

	"github.com/ADEXITUM/pikni/api/internal/locations/store"
)

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

func (s *Service) List(ctx context.Context) ([]store.ListLocationsRow, error) {
	return s.q.ListLocations(ctx)
}
