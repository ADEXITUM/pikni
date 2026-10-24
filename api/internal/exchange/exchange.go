// Package exchange ведёт обмен с ЕГАИС через порт egais.Gateway: отправляет очередь исходящих
// сообщений с повторами и забирает из УТМ точек ТТН, квитанции и ответы на запросы (ИН-3).
// Приём идемпотентен: сообщение, полученное повторно, второй раз не обрабатывается.
package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/egais/store"
)

type Exchanger struct {
	pool *pgxpool.Pool
	gw   egais.Gateway
	docs *documents.Service
	log  *slog.Logger
	now  func() time.Time
}

func New(pool *pgxpool.Pool, gw egais.Gateway, docs *documents.Service, log *slog.Logger) *Exchanger {
	return &Exchanger{pool: pool, gw: gw, docs: docs, log: log, now: time.Now}
}

// Run обменивается с УТМ раз в interval, пока не отменён ctx.
func (e *Exchanger) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := e.Once(ctx); err != nil && ctx.Err() == nil {
			e.log.Warn("egais exchange", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once — один круг обмена: сначала отправка, потом приём.
func (e *Exchanger) Once(ctx context.Context) error {
	return errors.Join(e.send(ctx), e.receive(ctx))
}

// backoff — пауза перед повтором: 5 с, 10 с, 20 с… не больше 5 минут.
func backoff(attempts int32) time.Duration {
	d := 5 * time.Second << min(attempts, 6)
	return min(d, 5*time.Minute)
}

func (e *Exchanger) send(ctx context.Context) error {
	return pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		msgs, err := q.ClaimQueued(ctx)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.FsrarID == nil {
				continue
			}
			reply, err := e.gw.Send(ctx, *m.FsrarID, m.DocType, m.Payload)
			if err != nil {
				// УТМ недоступен или ответил ошибкой: сообщение остаётся в очереди и уйдёт позже.
				text := err.Error()
				if errors.Is(err, egais.ErrUnavailable) {
					text = "УТМ недоступен"
				}
				if err := q.MarkRetry(ctx, store.MarkRetryParams{
					ID: m.ID, LastError: &text, NextAttemptAt: e.now().Add(backoff(m.Attempts)),
				}); err != nil {
					return err
				}
				continue
			}
			if err := q.MarkSent(ctx, store.MarkSentParams{ID: m.ID, ExternalID: &reply}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *Exchanger) receive(ctx context.Context) error {
	locs, err := store.New(e.pool).ExchangeLocations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range locs {
		msgs, err := e.gw.Inbox(ctx, l.FsrarID)
		if errors.Is(err, egais.ErrUnavailable) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, m := range msgs {
			if err := e.handle(ctx, l, m); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := e.gw.Ack(ctx, l.FsrarID, m.ID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// handle обрабатывает входящее сообщение в одной транзакции с отметкой о его получении:
// повторно полученное сообщение пропускается.
func (e *Exchanger) handle(ctx context.Context, l store.ExchangeLocationsRow, m egais.Inbound) error {
	return pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		id, err := q.InsertInbound(ctx, store.InsertInboundParams{
			LocationID: l.ID, DocType: m.Type, ExternalID: &m.ID, Payload: m.Payload,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		switch m.Type {
		case egais.WayBill:
			var ttn egais.TTN
			if err := json.Unmarshal(m.Payload, &ttn); err != nil {
				return err
			}
			doc, err := e.docs.CreateReceipt(ctx, tx, l.FsrarID, ttn)
			if err != nil {
				return err
			}
			if doc != [16]byte{} {
				return q.LinkInbound(ctx, store.LinkInboundParams{ID: id, DocumentID: &doc})
			}
		case egais.Ticket:
			var t egais.TicketReply
			if err := json.Unmarshal(m.Payload, &t); err != nil {
				return err
			}
			sent, err := q.FindSent(ctx, store.FindSentParams{LocationID: l.ID, ExternalID: &t.ReplyID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			status := "accepted"
			if t.Result != "accepted" {
				status = "rejected"
			}
			if err := q.SetTicket(ctx, store.SetTicketParams{ID: sent.ID, Status: status, Ticket: m.Payload}); err != nil {
				return err
			}
			if sent.DocumentID != nil {
				if err := q.LinkInbound(ctx, store.LinkInboundParams{ID: id, DocumentID: sent.DocumentID}); err != nil {
					return err
				}
				return e.docs.ApplyTicket(ctx, tx, *sent.DocumentID, status == "accepted")
			}
		case egais.ReplyRests:
			return q.InsertRestsSnapshot(ctx, store.InsertRestsSnapshotParams{LocationID: l.ID, Payload: m.Payload})
		}
		return nil
	})
}
