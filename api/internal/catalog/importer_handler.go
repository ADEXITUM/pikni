package catalog

import (
	"context"
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

// readUpload читает из формы поле file, но не больше лимита: файл крупнее не загружается целиком в память.
func readUpload(r *multipart.Reader) (string, []byte, error) {
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return "", nil, ErrImportEmpty
		}
		if err != nil {
			return "", nil, ErrImportFormat
		}
		if part.FormName() != "file" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, MaxImportBytes+1))
		if err != nil {
			return "", nil, ErrImportFormat
		}
		if len(data) > MaxImportBytes {
			return "", nil, ErrImportTooLarge
		}
		return part.FileName(), data, nil
	}
}

func (h *Handler) importUpload(ctx context.Context, body *multipart.Reader, commit bool) (apispec.ImportReport, error) {
	name, data, err := readUpload(body)
	if err != nil {
		return apispec.ImportReport{}, problem(err)
	}
	var setBy *uuid.UUID
	if sess, ok := auth.FromContext(ctx); ok {
		setBy = &sess.UserID
	}
	r, err := h.svc.Import(ctx, name, data, commit, setBy)
	if err != nil {
		return apispec.ImportReport{}, problem(err)
	}
	errs := make([]apispec.ImportError, len(r.Errors))
	for i, e := range r.Errors {
		errs[i] = apispec.ImportError{Row: e.Row, Message: e.Message}
	}
	return apispec.ImportReport{
		Committed: r.Committed, Rows: r.Rows, Products: r.Products, Variants: r.Variants,
		NewProducers: r.NewProducers, NewBrands: r.NewBrands, NewLines: r.NewLines, Errors: errs,
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
