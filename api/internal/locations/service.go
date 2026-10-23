// Package locations — точки сети: склады и магазины, их реквизиты, режим работы и зоны учёта.
package locations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/locations/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrNotFound        = errors.New("location not found")
	ErrConflict        = errors.New("location was changed by someone else")
	ErrInvalidCode     = errors.New("invalid location code")
	ErrCodeTaken       = errors.New("location code is already taken")
	ErrInvalidKind     = errors.New("unknown location kind")
	ErrEmptyField      = errors.New("name and city are required")
	ErrInvalidTimezone = errors.New("unknown time zone")
	ErrUnknownSubject  = errors.New("unknown subject of the Russian Federation")
	ErrInvalidFSRARID  = errors.New("FSRAR ID must be 12 digits")
	ErrFSRARIDTaken    = errors.New("FSRAR ID belongs to another location")
	ErrInvalidKPP      = errors.New("KPP must be 9 digits")
	ErrCoordinates     = errors.New("coordinates must be set together and be in range")
	ErrOpeningHours    = errors.New("invalid opening hours")
)

var (
	codePattern  = regexp.MustCompile(`^[А-ЯЁA-Z0-9-]{2,16}$`)
	fsrarPattern = regexp.MustCompile(`^[0-9]{12}$`)
	kppPattern   = regexp.MustCompile(`^[0-9]{9}$`)
)

type (
	Location = store.GetLocationRow
	Zone     = store.Zone
	Subject  = store.RuSubject
)

type Card struct {
	Location
	Zones []Zone
}

// Fields — то, что задаётся в карточке точки. Код и тип задаются только при создании.
type Fields struct {
	Name, City, Timezone, SubjectCode string
	Address, FSRARID, KPP, LicenseNo  *string
	Lat, Lon                          *float64
	LicenseValidTo                    *time.Time
	OpeningHours                      apispec.OpeningHours
	IsPublic                          bool
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func (s *Service) List(ctx context.Context) ([]Card, error) {
	rows, err := s.q.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	zones, err := s.q.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	byLocation := map[uuid.UUID][]Zone{}
	for _, z := range zones {
		byLocation[z.LocationID] = append(byLocation[z.LocationID], z)
	}

	cards := make([]Card, len(rows))
	for i, r := range rows {
		cards[i] = Card{Location: Location(r), Zones: byLocation[r.ID]}
	}
	return cards, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Card, error) {
	return getCard(ctx, s.q, id)
}

func (s *Service) Subjects(ctx context.Context) ([]Subject, error) {
	return s.q.ListRuSubjects(ctx)
}

func (s *Service) Create(ctx context.Context, code string, kind apispec.LocationKind, f Fields) (Card, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !codePattern.MatchString(code) {
		return Card{}, ErrInvalidCode
	}
	if !kind.Valid() {
		return Card{}, ErrInvalidKind
	}
	f, hours, err := f.normalize()
	if err != nil {
		return Card{}, err
	}

	var card Card
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err := q.CreateLocation(ctx, store.CreateLocationParams{
			Code: code, Kind: string(kind), Name: f.Name, City: f.City, Timezone: f.Timezone,
			SubjectCode: f.SubjectCode, Address: f.Address, Lat: f.Lat, Lon: f.Lon,
			FSRARID: f.FSRARID, KPP: f.KPP, LicenseNo: f.LicenseNo, LicenseValidTo: f.LicenseValidTo,
			OpeningHours: hours, IsPublic: f.IsPublic,
		})
		if err != nil {
			return writeError(err)
		}
		err = audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionLocationCreated,
			EntityType: audit.EntityLocation,
			EntityID:   id.String(),
			Data:       map[string]any{"code": code, "name": f.Name, "kind": kind},
		})
		if err != nil {
			return err
		}
		card, err = getCard(ctx, q, id)
		return err
	})
	return card, err
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, f Fields, isActive bool, version int32) (Card, error) {
	f, hours, err := f.normalize()
	if err != nil {
		return Card{}, err
	}

	var card Card
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.LockLocation(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock location: %w", err)
		}
		before := Location(row)
		if before.Version != version {
			return ErrConflict
		}

		err = q.UpdateLocation(ctx, store.UpdateLocationParams{
			ID: id, Name: f.Name, City: f.City, Timezone: f.Timezone, SubjectCode: f.SubjectCode,
			Address: f.Address, Lat: f.Lat, Lon: f.Lon, FSRARID: f.FSRARID, KPP: f.KPP,
			LicenseNo: f.LicenseNo, LicenseValidTo: f.LicenseValidTo, OpeningHours: hours,
			IsPublic: f.IsPublic, IsActive: isActive,
		})
		if err != nil {
			return writeError(err)
		}

		after := before
		after.Name, after.City, after.Timezone, after.SubjectCode = f.Name, f.City, f.Timezone, f.SubjectCode
		after.Address, after.Lat, after.Lon, after.FSRARID, after.KPP = f.Address, f.Lat, f.Lon, f.FSRARID, f.KPP
		after.LicenseNo, after.LicenseValidTo, after.OpeningHours = f.LicenseNo, f.LicenseValidTo, hours
		after.IsPublic, after.IsActive = f.IsPublic, isActive
		if changes := diff(before, after); len(changes) > 0 {
			err := audit.Record(ctx, tx, audit.Event{
				Action:     apispec.AuditActionLocationUpdated,
				EntityType: audit.EntityLocation,
				EntityID:   id.String(),
				Data:       map[string]any{"code": before.Code, "changes": changes},
			})
			if err != nil {
				return err
			}
		}
		card, err = getCard(ctx, q, id)
		return err
	})
	return card, err
}

func getCard(ctx context.Context, q *store.Queries, id uuid.UUID) (Card, error) {
	row, err := q.GetLocation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, fmt.Errorf("get location: %w", err)
	}
	zones, err := q.ListLocationZones(ctx, id)
	if err != nil {
		return Card{}, fmt.Errorf("list zones: %w", err)
	}
	return Card{Location: row, Zones: zones}, nil
}

// normalize проверяет поля карточки и возвращает режим работы в виде JSON для базы.
func (f Fields) normalize() (Fields, []byte, error) {
	f.Name, f.City = strings.TrimSpace(f.Name), strings.TrimSpace(f.City)
	f.Address, f.FSRARID, f.KPP, f.LicenseNo = trimmed(f.Address), trimmed(f.FSRARID), trimmed(f.KPP), trimmed(f.LicenseNo)

	switch {
	case f.Name == "" || f.City == "":
		return f, nil, ErrEmptyField
	case !validTimezone(f.Timezone):
		return f, nil, ErrInvalidTimezone
	case f.FSRARID != nil && !fsrarPattern.MatchString(*f.FSRARID):
		return f, nil, ErrInvalidFSRARID
	case f.KPP != nil && !kppPattern.MatchString(*f.KPP):
		return f, nil, ErrInvalidKPP
	case (f.Lat == nil) != (f.Lon == nil),
		f.Lat != nil && (*f.Lat < -90 || *f.Lat > 90 || *f.Lon < -180 || *f.Lon > 180):
		return f, nil, ErrCoordinates
	case !validHours(f.OpeningHours):
		return f, nil, ErrOpeningHours
	}

	hours, err := json.Marshal(f.OpeningHours)
	if err != nil {
		return f, nil, fmt.Errorf("encode opening hours: %w", err)
	}
	return f, hours, nil
}

func validTimezone(name string) bool {
	if name == "" || name == "UTC" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func validHours(h apispec.OpeningHours) bool {
	for _, d := range []*apispec.DayHours{h.Mon, h.Tue, h.Wed, h.Thu, h.Fri, h.Sat, h.Sun} {
		if d == nil {
			continue
		}
		open, okOpen := minutes(d.Open)
		closing, okClose := minutes(d.Close)
		if !okOpen || !okClose || open >= closing {
			return false
		}
	}
	return true
}

// minutes разбирает «ЧЧ:ММ» в минуты от полуночи; «24:00» допустимо как конец дня.
func minutes(s string) (int, bool) {
	hh, mm, ok := strings.Cut(s, ":")
	h, errH := strconv.Atoi(hh)
	m, errM := strconv.Atoi(mm)
	if !ok || len(hh) != 2 || len(mm) != 2 || errH != nil || errM != nil || m > 59 || h > 24 || (h == 24 && m > 0) {
		return 0, false
	}
	return h*60 + m, true
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func writeError(err error) error {
	switch {
	case db.IsUniqueViolationOn(err, "locations_code_key"):
		return ErrCodeTaken
	case db.IsUniqueViolationOn(err, "locations_fsrar_id_key"):
		return ErrFSRARIDTaken
	case db.IsForeignKeyViolation(err):
		return ErrUnknownSubject
	}
	return fmt.Errorf("write location: %w", err)
}

// diff возвращает изменившиеся поля карточки в виде «было, стало» для журнала.
func diff(before, after Location) map[string]any {
	fields := func(l Location) map[string]any {
		var hours apispec.OpeningHours
		_ = json.Unmarshal(l.OpeningHours, &hours)
		var validTo *string
		if l.LicenseValidTo != nil {
			d := l.LicenseValidTo.Format(time.DateOnly)
			validTo = &d
		}
		return map[string]any{
			"name": l.Name, "city": l.City, "timezone": l.Timezone, "subjectCode": l.SubjectCode,
			"address": l.Address, "lat": l.Lat, "lon": l.Lon, "fsrarId": l.FSRARID, "kpp": l.KPP,
			"licenseNo": l.LicenseNo, "licenseValidTo": validTo, "openingHours": hours,
			"isPublic": l.IsPublic, "isActive": l.IsActive,
		}
	}

	was, now := fields(before), fields(after)
	changes := map[string]any{}
	for key, old := range was {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(now[key])
		if !bytes.Equal(a, b) {
			changes[key] = []any{old, now[key]}
		}
	}
	return changes
}
