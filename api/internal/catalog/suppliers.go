package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrInvalidKPP           = errors.New("KPP must be 9 digits and only for organizations")
	ErrInvalidFSRARID       = errors.New("FSRAR ID must be 12 digits")
	ErrFSRARIDTaken         = errors.New("FSRAR ID belongs to another supplier")
	ErrSupplierExists       = errors.New("supplier with this INN and KPP already exists")
	ErrInvalidReceivingMode = errors.New("unknown receiving mode")
	ErrInvalidEmail         = errors.New("invalid email")
)

var (
	kppPattern   = regexp.MustCompile(`^[0-9]{9}$`)
	fsrarPattern = regexp.MustCompile(`^[0-9]{12}$`)
)

func (s *Service) Suppliers(ctx context.Context) ([]store.Supplier, error) {
	return s.q.ListSuppliers(ctx)
}

func supplierContacts(raw []byte) apispec.SupplierContacts {
	var c apispec.SupplierContacts
	_ = json.Unmarshal(raw, &c)
	return c
}

func supplierFields(sp store.Supplier) map[string]any {
	c := supplierContacts(sp.Contacts)
	return map[string]any{
		"name": sp.Name, "inn": sp.INN, "kpp": sp.KPP, "fsrarId": sp.FSRARID, "licenseNo": sp.LicenseNo,
		"licenseValidTo": sp.LicenseValidTo, "receivingMode": sp.DefaultReceivingMode,
		"contactPerson": c.Person, "phone": c.Phone, "email": c.Email, "isActive": sp.IsActive,
	}
}

// supplierParams проверяет карточку поставщика. У предпринимателя (ИНН из 12 цифр) КПП нет.
func supplierParams(f apispec.SupplierFields) (store.CreateSupplierParams, error) {
	var p store.CreateSupplierParams
	n, err := name(f.Name)
	if err != nil {
		return p, err
	}
	inn := strings.TrimSpace(f.INN)
	if !innValid(inn) {
		return p, ErrInvalidINN
	}
	kpp := optional(f.KPP)
	if kpp != nil && (!kppPattern.MatchString(*kpp) || len(inn) == 12) {
		return p, ErrInvalidKPP
	}
	fsrar := optional(f.FSRARID)
	if fsrar != nil && !fsrarPattern.MatchString(*fsrar) {
		return p, ErrInvalidFSRARID
	}
	if !f.DefaultReceivingMode.Valid() {
		return p, ErrInvalidReceivingMode
	}
	contacts := apispec.SupplierContacts{Person: optional(f.Contacts.Person), Phone: optional(f.Contacts.Phone), Email: optional(f.Contacts.Email)}
	if contacts.Email != nil {
		if _, err := mail.ParseAddress(*contacts.Email); err != nil {
			return p, ErrInvalidEmail
		}
	}
	raw, err := json.Marshal(contacts)
	if err != nil {
		return p, err
	}
	p = store.CreateSupplierParams{
		Name: n, INN: inn, KPP: kpp, FSRARID: fsrar, LicenseNo: optional(f.LicenseNo),
		DefaultReceivingMode: string(f.DefaultReceivingMode), Contacts: raw,
	}
	if f.LicenseValidTo != nil {
		p.LicenseValidTo = &f.LicenseValidTo.Time
	}
	return p, nil
}

func supplierWriteError(err error) error {
	switch {
	case db.IsUniqueViolationOn(err, "suppliers_fsrar_id_key"):
		return ErrFSRARIDTaken
	case db.IsUniqueViolation(err):
		return ErrSupplierExists
	}
	return err
}

func (s *Service) SaveSupplier(ctx context.Context, id *uuid.UUID, f apispec.SupplierFields, active bool) (store.Supplier, error) {
	p, err := supplierParams(f)
	if err != nil {
		return store.Supplier{}, err
	}
	var saved store.Supplier
	err = s.write(ctx, "supplier", func(q *store.Queries) (change, error) {
		if id == nil {
			sp, err := q.CreateSupplier(ctx, p)
			saved = sp
			return change{id: sp.ID.String(), after: supplierFields(sp)}, supplierWriteError(err)
		}
		old, err := q.GetSupplier(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateSupplier(ctx, store.UpdateSupplierParams{
			ID: *id, Name: p.Name, INN: p.INN, KPP: p.KPP, FSRARID: p.FSRARID, LicenseNo: p.LicenseNo,
			LicenseValidTo: p.LicenseValidTo, DefaultReceivingMode: p.DefaultReceivingMode, Contacts: p.Contacts, IsActive: active,
		})
		return change{id: id.String(), before: supplierFields(old), after: supplierFields(saved)}, supplierWriteError(err)
	})
	return saved, err
}
