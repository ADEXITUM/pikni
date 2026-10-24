// Package egais — обмен с ЕГАИС через порт. Сейчас — только очередь исходящих сообщений:
// документ пишет сообщение в своей транзакции, отправит его адаптер к имитатору (итерация 5).
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
}

// Enqueue ставит сообщение в очередь в транзакции документа (transactional outbox):
// если документ откатится, сообщение не уйдёт.
func Enqueue(ctx context.Context, tx pgx.Tx, location uuid.UUID, docType string, document uuid.UUID, items []Item) error {
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = store.New(tx).EnqueueMessage(ctx, store.EnqueueMessageParams{
		LocationID: location, DocType: docType, DocumentID: &document, Payload: payload,
	})
	return err
}
