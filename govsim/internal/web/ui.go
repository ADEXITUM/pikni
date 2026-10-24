package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/govsim/internal/sim"
)

//go:embed templates/*.html
var templates embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"datamatrix": dataMatrix,
	// слоты строк формы поставки
	"slots": func() []int { return []int{1, 2, 3, 4} },
	"ean13": ean13,
	"sscc":  sscc,
	"json": func(v any) string {
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	},
	"pretty": func(raw json.RawMessage) string {
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return string(raw)
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	},
	"kind": func(k string) string {
		return map[string]string{"supplier": "поставщик", "retail_point": "магазин", "warehouse": "склад"}[k]
	},
	"status": func(st string) string {
		return map[string]string{
			"in_transit": "в пути", "on_balance": "на балансе", "sold": "продана", "written_off": "списана",
			"introduced": "в обороте", "blocked": "заблокирован", "received": "получен", "accepted": "принят",
			"rejected": "отклонён",
		}[st]
	},
}).ParseFS(templates, "templates/*.html"))

// basicAuth закрывает интерфейс паролем администратора стенда. Протокол для «Пикни» им не закрыт:
// он доступен только во внутренней сети.
func (s *Server) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(s.password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="govsim", charset="UTF-8"`)
			http.Error(w, "Нужен пароль администратора стенда", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

var titles = map[string]string{
	"index.html": "Участники и сценарии", "point.html": "Остатки точки", "mark.html": "Марка",
	"journal.html": "Журнал документов", "shipments.html": "Поставки", "print.html": "Печать поставки",
	"crpt.html": "«Честный знак»", "check.html": "Чек в ЕГАИС", "packs.html": "Пачки сигарет",
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	data["Title"] = titles[name]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render", "page", name, "error", err)
	}
}

func (s *Server) ui(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.basicAuth(s.index))
	mux.HandleFunc("POST /ui/participants/{fsrar}/scenario", s.basicAuth(s.scenario))
	mux.HandleFunc("GET /ui/points/{fsrar}", s.basicAuth(s.point))
	mux.HandleFunc("POST /ui/points/{fsrar}/rests", s.basicAuth(s.setRest))
	mux.HandleFunc("GET /ui/marks", s.basicAuth(s.mark))
	mux.HandleFunc("POST /ui/marks/block", s.basicAuth(s.blockMark))
	mux.HandleFunc("GET /ui/journal", s.basicAuth(s.journal))
	mux.HandleFunc("GET /ui/shipments", s.basicAuth(s.shipments))
	mux.HandleFunc("POST /ui/shipments", s.basicAuth(s.createShipment))
	mux.HandleFunc("GET /ui/shipments/{id}/print", s.basicAuth(s.print))
	mux.HandleFunc("GET /ui/crpt", s.basicAuth(s.crpt))
	mux.HandleFunc("POST /ui/crpt/batches", s.basicAuth(s.batch))
	mux.HandleFunc("POST /ui/crpt/packs", s.basicAuth(s.packs))
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	ps, err := s.sim.Participants(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "index.html", map[string]any{"Participants": ps})
}

func (s *Server) scenario(w http.ResponseWriter, r *http.Request) {
	delay, _ := strconv.Atoi(r.FormValue("delay"))
	if err := s.sim.SetScenario(r.Context(), r.PathValue("fsrar"), r.FormValue("utm_down") == "on", delay); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) point(w http.ResponseWriter, r *http.Request) {
	p, err := s.sim.Participant(r.Context(), r.PathValue("fsrar"))
	if err != nil {
		s.fail(w, err)
		return
	}
	rests, marks, err := s.sim.Rests(r.Context(), p.FSRARID)
	if err != nil {
		s.fail(w, err)
		return
	}
	products, err := s.sim.Products(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "point.html", map[string]any{"P": p, "Rests": rests, "Marks": marks, "Products": products})
}

func (s *Server) setRest(w http.ResponseWriter, r *http.Request) {
	register, _ := strconv.Atoi(r.FormValue("register"))
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	if register != 1 && register != 2 {
		register = 1
	}
	fsrar := r.PathValue("fsrar")
	if err := s.sim.SetRest(r.Context(), fsrar, r.FormValue("alcocode"), register, qty); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/ui/points/"+fsrar, http.StatusSeeOther)
}

func (s *Server) mark(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Code": strings.ToUpper(strings.TrimSpace(r.FormValue("code")))}
	if code := data["Code"].(string); code != "" {
		m, err := s.sim.Mark(r.Context(), code)
		if err == nil {
			data["Mark"] = m
		} else {
			data["Missing"] = true
		}
	}
	s.render(w, "mark.html", data)
}

func (s *Server) blockMark(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if err := s.sim.SetMarkBlocked(r.Context(), code, r.FormValue("blocked") == "on"); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/ui/marks?code="+code, http.StatusSeeOther)
}

func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	fsrar := r.FormValue("fsrar")
	entries, err := s.sim.Journal(r.Context(), fsrar)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "journal.html", map[string]any{"Entries": entries, "FSRAR": fsrar})
}

func (s *Server) shipments(w http.ResponseWriter, r *http.Request) {
	list, err := s.sim.Shipments(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	ps, err := s.sim.Participants(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	products, err := s.sim.Products(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "shipments.html", map[string]any{
		"Shipments": list, "Participants": ps, "Products": products, "Error": r.FormValue("error"),
	})
}

func (s *Server) createShipment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	num := func(name string) int {
		n, _ := strconv.Atoi(r.FormValue(name))
		return max(n, 0)
	}
	req := sim.ShipmentRequest{
		Supplier: r.FormValue("supplier"), Recipient: r.FormValue("recipient"), BoxSize: num("box_size"),
		Shortage: num("shortage"), Surplus: num("surplus"), ForeignMark: r.FormValue("foreign_mark") == "on",
	}
	for i, code := range r.Form["alcocode"] {
		if i < len(r.Form["qty"]) && code != "" {
			qty, _ := strconv.Atoi(r.Form["qty"][i])
			req.Positions = append(req.Positions, sim.Item{Alcocode: code, Qty: qty})
		}
	}
	if from, to := r.FormValue("mixup_from"), r.FormValue("mixup_to"); from != "" && to != "" && num("mixup_qty") > 0 {
		req.Mixup = &sim.Mixup{From: from, To: to, Qty: num("mixup_qty")}
	}
	sh, err := s.sim.CreateShipment(r.Context(), req)
	if err != nil {
		http.Redirect(w, r, "/ui/shipments?error="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ui/shipments/"+sh.ID.String()+"/print", http.StatusSeeOther)
}

func (s *Server) print(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sh, err := s.sim.Shipment(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "print.html", map[string]any{"S": sh})
}

func (s *Server) crpt(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Code": strings.TrimSpace(r.FormValue("code"))}
	if code := data["Code"].(string); code != "" {
		// Принимается и полный код со сканера: (01) GTIN (21) серийный номер.
		if strings.HasPrefix(code, "01") && len(code) >= 25 && code[16:18] == "21" {
			code = code[2:16] + strings.SplitN(code[18:], "\x1d", 2)[0]
		}
		if info, err := s.sim.Code(r.Context(), code); err == nil {
			data["Info"] = info
		} else {
			data["Missing"] = true
		}
	}
	batches, err := s.sim.Batches(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	data["Batches"] = batches
	if data["Points"], err = s.retailPoints(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "crpt.html", data)
}

func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.FormValue("action") {
	case "block":
		err = s.sim.BlockBatch(r.Context(), r.FormValue("batch"), "Отзыв партии производителем")
	case "expire":
		err = s.sim.ExpireBatch(r.Context(), r.FormValue("batch"))
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/ui/crpt", http.StatusSeeOther)
}

func (s *Server) retailPoints(ctx context.Context) ([]sim.Participant, error) {
	ps, err := s.sim.Participants(ctx)
	out := ps[:0]
	for _, p := range ps {
		if p.Kind == "retail_point" {
			out = append(out, p)
		}
	}
	return out, err
}

// packs выпускает пачки сигарет для точки и показывает их коды для печати или скана с экрана.
func (s *Server) packs(w http.ResponseWriter, r *http.Request) {
	rub, _ := strconv.ParseFloat(strings.ReplaceAll(r.FormValue("mrp"), ",", "."), 64)
	n, _ := strconv.Atoi(r.FormValue("count"))
	mrp := int64(math.Round(rub * 100))
	codes, err := s.sim.CreatePacks(r.Context(), r.FormValue("owner"), strings.TrimSpace(r.FormValue("gtin")), mrp, n)
	if errors.Is(err, sim.ErrBadPacks) {
		http.Error(w, "GTIN — 14 цифр, МРЦ и количество — больше нуля (не больше 200 пачек)", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "packs.html", map[string]any{"Codes": codes, "MRP": fmt.Sprintf("%d,%02d", mrp/100, mrp%100), "Owner": r.FormValue("owner")})
}
