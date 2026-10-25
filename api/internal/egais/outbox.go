// Package egais — обмен с ЕГАИС через порт Gateway: очередь исходящих сообщений (transactional
// outbox), формат документов и клиент УТМ имитатора. Отправку и приём ведёт пакет exchange.
package egais

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/egais/store"
)

// Виды исходящих документов ЕГАИС.
const (
	// TransferToShop — перевод продукции без марок со склада (регистр 1) в торговый зал (регистр 2).
	TransferToShop = "TransferToShop"
	// TransferFromShop — возврат из торгового зала на склад.
	TransferFromShop = "TransferFromShop"
)

// Item — позиция сообщения: вариант, его алкокод и количество.
type Item struct {
	VariantID uuid.UUID `json:"variantId"`
	Alcocode  *string   `json:"alcocode,omitempty"`
	Qty       int32     `json:"qty"`
	// Marks — марки списываемой продукции с помарочным учётом.
	Marks []string `json:"marks,omitempty"`
}

// Enqueue ставит сообщение в очередь в транзакции документа (transactional outbox):
// если документ откатится, сообщение не уйдёт, а если упадёт отправка — документ не потеряет его.
func Enqueue(ctx context.Context, tx pgx.Tx, location uuid.UUID, docType string, document uuid.UUID, items []Item) error {
	return EnqueuePayload(ctx, tx, location, docType, document, map[string]any{"items": items})
}

// EnqueuePayload — то же для документа произвольной структуры, например акта по ТТН.
func EnqueuePayload(ctx context.Context, tx pgx.Tx, location uuid.UUID, docType string, document uuid.UUID, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = store.New(tx).EnqueueMessage(ctx, store.EnqueueMessageParams{
		LocationID: location, DocType: docType, DocumentID: &document, Payload: payload,
	})
	return err
}

// Pending — сколько исходящих сообщений документа ещё ждут квитанции и сколько отклонено.
func Pending(ctx context.Context, tx pgx.Tx, document uuid.UUID) (pending, rejected int32, err error) {
	row, err := store.New(tx).DocumentPending(ctx, &document)
	return row.Pending, row.Rejected, err
}

// Exchange — журнал обмена документа с ЕГАИС для его карточки (ИН-3).
func Exchange(ctx context.Context, db store.DBTX, document uuid.UUID) ([]store.DocumentExchangeRow, error) {
	return store.New(db).DocumentExchange(ctx, &document)
}

// EnqueueStandalone ставит в очередь сообщение без документа — запрос остатков для сверки.
func EnqueueStandalone(ctx context.Context, db store.DBTX, location uuid.UUID, docType string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = store.New(db).EnqueueMessage(ctx, store.EnqueueMessageParams{LocationID: location, DocType: docType, Payload: payload})
	return err
}
