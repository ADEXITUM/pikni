// Package crpt — порт ГИС МТ «Честный знак»: проверка кодов перед продажей (разрешительный режим),
// выбытие при продаже и возврат в оборот. На стенде его реализует клиент имитатора.
package crpt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrUnavailable = errors.New("crpt is unavailable")

// Verdict — можно ли продать код и почему нет: code.unknown, code.sold, code.blocked,
// code.expired, code.not_owner.
type Verdict struct {
	Code    string `json:"code"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type Gateway interface {
	Check(ctx context.Context, fsrar string, codes []string) ([]Verdict, error)
	Sell(ctx context.Context, fsrar string, codes []string) error
	Return(ctx context.Context, fsrar string, codes []string) error
}

type Govsim struct {
	base   string
	client *http.Client
}

func NewGovsim(base string) *Govsim {
	return &Govsim{base: base, client: &http.Client{Timeout: 3 * time.Second}}
}

func (g *Govsim) post(ctx context.Context, path, fsrar string, codes []string, out any) error {
	body, err := json.Marshal(map[string]any{"fsrar_id": fsrar, "codes": codes})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %s %d", ErrUnavailable, path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (g *Govsim) Check(ctx context.Context, fsrar string, codes []string) ([]Verdict, error) {
	var out struct {
		Codes []Verdict `json:"codes"`
	}
	err := g.post(ctx, "/crpt/codes/check", fsrar, codes, &out)
	return out.Codes, err
}

func (g *Govsim) Sell(ctx context.Context, fsrar string, codes []string) error {
	return g.post(ctx, "/crpt/sales", fsrar, codes, nil)
}

func (g *Govsim) Return(ctx context.Context, fsrar string, codes []string) error {
	return g.post(ctx, "/crpt/returns", fsrar, codes, nil)
}
