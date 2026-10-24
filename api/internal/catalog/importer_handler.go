package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
)

func (h *Handler) GetImportTemplate(_ context.Context, _ apispec.GetImportTemplateRequestObject) (apispec.GetImportTemplateResponseObject, error) {
	file, err := ImportTemplate()
	if err != nil {
		return nil, err
	}
	disposition := `attachment; filename="pikni-import-template.xlsx"`
	return apispec.GetImportTemplate200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		Body:          file,
		ContentLength: int64(file.Len()),
		Headers:       apispec.GetImportTemplate200ResponseHeaders{ContentDisposition: &disposition},
	}, nil
}

// importForm — поля формы импорта: файл и принятые расхождения.
type importForm struct {
	name   string
	data   []byte
	accept map[string]bool
}

// readUpload читает из формы файл, но не больше лимита — файл крупнее не загружается целиком
// в память, — и список принятых расхождений.
func readUpload(r *multipart.Reader) (importForm, error) {
	form := importForm{accept: map[string]bool{}}
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return form, ErrImportFormat
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, MaxImportBytes+1))
			if err != nil {
				return form, ErrImportFormat
			}
			if len(data) > MaxImportBytes {
				return form, ErrImportTooLarge
			}
			form.name, form.data = part.FileName(), data
		case "accept":
			var keys []string
			if err := json.NewDecoder(io.LimitReader(part, MaxImportBytes)).Decode(&keys); err != nil {
				return form, ErrImportFormat
			}
			for _, k := range keys {
				form.accept[k] = true
			}
		}
	}
	if form.data == nil {
		return form, ErrImportEmpty
	}
	return form, nil
}

func (h *Handler) importUpload(ctx context.Context, body *multipart.Reader, commit bool) (apispec.ImportReport, error) {
	form, err := readUpload(body)
	if err != nil {
		return apispec.ImportReport{}, problem(err)
	}
	var setBy *uuid.UUID
	if sess, ok := auth.FromContext(ctx); ok {
		setBy = &sess.UserID
	}
	r, err := h.svc.Import(ctx, form.name, form.data, commit, setBy, form.accept)
	if err != nil {
		return apispec.ImportReport{}, problem(err)
	}
	errs := make([]apispec.ImportError, len(r.Errors))
	for i, e := range r.Errors {
		errs[i] = apispec.ImportError{Row: e.Row, Message: e.Message}
	}
	changes := make([]apispec.ImportChange, len(r.Changes))
	for i, c := range r.Changes {
		changes[i] = apispec.ImportChange{
			Key: c.Key, Row: c.Row, Product: c.Product, Variant: textOrNil(c.Variant), Field: c.Field, Old: c.Old, New: c.New,
		}
	}
	return apispec.ImportReport{
		Committed: r.Committed, Rows: r.Rows, Products: r.Products, Existing: r.Existing, Variants: r.Variants,
		NewProducers: r.NewProducers, NewBrands: r.NewBrands, NewLines: r.NewLines, Changes: changes, Errors: errs,
	}, nil
}

func (h *Handler) PreviewImport(ctx context.Context, req apispec.PreviewImportRequestObject) (apispec.PreviewImportResponseObject, error) {
	r, err := h.importUpload(ctx, req.Body, false)
	if err != nil {
		return nil, err
	}
	return apispec.PreviewImport200JSONResponse(r), nil
}

func (h *Handler) RunImport(ctx context.Context, req apispec.RunImportRequestObject) (apispec.RunImportResponseObject, error) {
	r, err := h.importUpload(ctx, req.Body, true)
	if err != nil {
		return nil, err
	}
	return apispec.RunImport200JSONResponse(r), nil
}
