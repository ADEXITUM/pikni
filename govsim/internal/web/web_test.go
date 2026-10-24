package web_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/govsim/internal/simtest"
	"github.com/ADEXITUM/pikni/govsim/internal/web"
)

// Страницы интерфейса открываются под паролем и рендерятся без ошибок шаблонов; протокол
// УТМ отвечает 503, когда УТМ точки «выключен».
func TestPagesAndProtocol(t *testing.T) {
	s := simtest.New(t)
	srv := httptest.NewServer(web.New(s, slog.New(slog.DiscardHandler), "secret").Handler())
	defer srv.Close()

	get := func(path string, auth bool) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if auth {
			req.SetBasicAuth("admin", "secret")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, _ := get("/", false); code != http.StatusUnauthorized {
		t.Errorf("index without password: %d", code)
	}

	body := `{"supplier":"` + simtest.Supplier + `","recipient":"` + simtest.Store +
		`","positions":[{"alcocode":"` + simtest.Vodka + `","qty":3},{"alcocode":"` + simtest.Beer + `","qty":2}]}`
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/supplier/shipments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("shipment: %v %v", resp, err)
	}
	_ = resp.Body.Close()

	for _, path := range []string{"/", "/ui/points/" + simtest.Store, "/ui/journal", "/ui/shipments", "/ui/crpt", "/ui/marks?code=PKN0"} {
		if code, body := get(path, true); code != http.StatusOK || !strings.Contains(body, "</html>") {
			t.Errorf("%s: %d %.200s", path, code, body)
		}
	}
	_, list := get("/ui/shipments", true)
	start := strings.Index(list, "/ui/shipments/")
	link := list[start : start+strings.Index(list[start:], `"`)]
	if code, body := get(link, true); code != http.StatusOK || !strings.Contains(body, "data:image/png;base64,") {
		t.Errorf("print page: %d", code)
	}

	if code, body := get("/utm/"+simtest.Store+"/opt/out", false); code != http.StatusOK || !strings.Contains(body, "WayBill") {
		t.Errorf("outbox: %d %.200s", code, body)
	}
	if err := s.SetScenario(t.Context(), simtest.Store, true, 0); err != nil {
		t.Fatal(err)
	}
	if code, _ := get("/utm/"+simtest.Store+"/status", false); code != http.StatusServiceUnavailable {
		t.Errorf("status with UTM down: %d", code)
	}
}
