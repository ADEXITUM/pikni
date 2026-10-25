package egais

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ErrUnavailable — УТМ точки не отвечает: сообщение остаётся в очереди и уйдёт позже.
var ErrUnavailable = errors.New("utm is unavailable")

// Inbound — сообщение из «исходящих» УТМ точки: ТТН, квитанция или ответ на запрос.
type Inbound struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Gateway — порт ЕГАИС (п. 4.1 backend.md). Модули знают только его; на стенде его реализует
// клиент имитатора, настоящий УТМ подключился бы другой реализацией без изменения учёта.
type Gateway interface {
	Send(ctx context.Context, fsrar, docType string, payload []byte) (replyID string, err error)
	Inbox(ctx context.Context, fsrar string) ([]Inbound, error)
	Ack(ctx context.Context, fsrar, id string) error
	// Cheque — чек с марками: синхронно, как касса работает с УТМ при оплате.
	Cheque(ctx context.Context, fsrar string, c Cheque) (ChequeReply, error)
}

// Cheque — чек продажи или возврата с марками.
type Cheque struct {
	Kind      string   `json:"kind"`
	ReceiptID string   `json:"receipt_id"`
	Marks     []string `json:"marks"`
}

// ChequeReply — ответ ЕГАИС: QR-ссылка или отказ с причиной.
type ChequeReply struct {
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
	QRURL  string `json:"qr_url,omitempty"`
}

// Govsim — клиент УТМ имитатора (docs/design/govsim.md, п. 3).
type Govsim struct {
	base   string
	client *http.Client
}

func NewGovsim(base string) *Govsim {
	return &Govsim{base: base, client: &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}}
}

// chequeTimeout — касса ждёт ответа ЕГАИС на чек не дольше 3 с (п. 4.3 backend.md).
const chequeTimeout = 3 * time.Second

func (g *Govsim) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		// Нет связи с УТМ — то же, что недоступный УТМ: повторить позже.
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusServiceUnavailable:
		return ErrUnavailable
	case resp.StatusCode >= 300:
		return fmt.Errorf("utm %s %s: %d %s", method, path, resp.StatusCode, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (g *Govsim) Send(ctx context.Context, fsrar, docType string, payload []byte) (string, error) {
	var out struct {
		ReplyID string `json:"reply_id"`
	}
	err := g.do(ctx, http.MethodPost, "/utm/"+url.PathEscape(fsrar)+"/opt/in/"+url.PathEscape(docType), payload, &out)
	return out.ReplyID, err
}

func (g *Govsim) Inbox(ctx context.Context, fsrar string) ([]Inbound, error) {
	var out struct {
		Messages []Inbound `json:"messages"`
	}
	err := g.do(ctx, http.MethodGet, "/utm/"+url.PathEscape(fsrar)+"/opt/out", nil, &out)
	return out.Messages, err
}

func (g *Govsim) Ack(ctx context.Context, fsrar, id string) error {
	return g.do(ctx, http.MethodDelete, "/utm/"+url.PathEscape(fsrar)+"/opt/out/"+url.PathEscape(id), nil, nil)
}

func (g *Govsim) Cheque(ctx context.Context, fsrar string, c Cheque) (ChequeReply, error) {
	ctx, cancel := context.WithTimeout(ctx, chequeTimeout)
	defer cancel()
	body, err := json.Marshal(c)
	if err != nil {
		return ChequeReply{}, err
	}
	var out ChequeReply
	err = g.do(ctx, http.MethodPost, "/utm/"+url.PathEscape(fsrar)+"/cheque", body, &out)
	return out, err
}
