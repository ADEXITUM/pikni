// Package web — HTTP имитатора: протокол для «Пикни» (УТМ точек, ГИС МТ, поставщик) и веб-интерфейс
// «глазами ЕГАИС». Протокол открыт только во внутренней сети стенда, интерфейс — под паролем.
package web

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/govsim/internal/sim"
)

type Server struct {
	sim       *sim.Sim
	log       *slog.Logger
	password  string
	publicURL string
}

// New собирает сервер. publicURL — адрес имитатора для ссылок в QR-кодах чеков.
func New(s *sim.Sim, log *slog.Logger, password, publicURL string) *Server {
	return &Server{sim: s, log: log, password: password, publicURL: publicURL}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })

	mux.HandleFunc("GET /utm/{fsrar}/status", s.utmStatus)
	mux.HandleFunc("POST /utm/{fsrar}/opt/in/{type}", s.utmIn)
	mux.HandleFunc("GET /utm/{fsrar}/opt/out", s.utmOut)
	mux.HandleFunc("GET /utm/{fsrar}/opt/out/{id}", s.utmMessage)
	mux.HandleFunc("DELETE /utm/{fsrar}/opt/out/{id}", s.utmAck)
	mux.HandleFunc("POST /utm/{fsrar}/cheque", s.cheque)
	mux.HandleFunc("GET /check/{receipt}", s.checkPage)

	mux.HandleFunc("POST /crpt/codes/check", s.crptCheck)
	mux.HandleFunc("POST /crpt/sales", s.crptSales)
	mux.HandleFunc("POST /crpt/returns", s.crptReturns)

	mux.HandleFunc("POST /supplier/shipments", s.shipment)

	s.ui(mux)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail переводит ошибку имитатора в ответ: «УТМ недоступен» — 503, как у выключенного УТМ.
func (s *Server) fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, sim.ErrUTMDown):
		status = http.StatusServiceUnavailable
	case errors.Is(err, sim.ErrUnknownParticipant), errors.Is(err, sim.ErrNotFound):
		status = http.StatusNotFound
	}
	if status == http.StatusInternalServerError {
		s.log.Error("request failed", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) utmStatus(w http.ResponseWriter, r *http.Request) {
	p, err := s.sim.Status(r.Context(), r.PathValue("fsrar"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fsrar_id": p.FSRARID, "name": p.Name, "status": "up"})
}

func (s *Server) utmIn(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id, err := s.sim.Receive(r.Context(), r.PathValue("fsrar"), r.PathValue("type"), body)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"reply_id": id.String()})
}

func (s *Server) utmOut(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.sim.Outbox(r.Context(), r.PathValue("fsrar"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if msgs == nil {
		msgs = []sim.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) messageID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad id"})
		return id, false
	}
	return id, true
}

func (s *Server) utmMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.messageID(w, r)
	if !ok {
		return
	}
	m, err := s.sim.Message(r.Context(), r.PathValue("fsrar"), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) utmAck(w http.ResponseWriter, r *http.Request) {
	id, ok := s.messageID(w, r)
	if !ok {
		return
	}
	if err := s.sim.Ack(r.Context(), r.PathValue("fsrar"), id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type codesRequest struct {
	FSRARID string   `json:"fsrar_id"`
	Codes   []string `json:"codes"`
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return v, false
	}
	return v, true
}

func (s *Server) crptCheck(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[codesRequest](w, r)
	if !ok {
		return
	}
	verdicts, err := s.sim.CheckCodes(r.Context(), req.FSRARID, req.Codes)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": verdicts})
}

func (s *Server) crptSales(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[codesRequest](w, r)
	if !ok {
		return
	}
	if err := s.sim.SellCodes(r.Context(), req.FSRARID, req.Codes); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) crptReturns(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[codesRequest](w, r)
	if !ok {
		return
	}
	if err := s.sim.ReturnCodes(r.Context(), req.FSRARID, req.Codes); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) shipment(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[sim.ShipmentRequest](w, r)
	if !ok {
		return
	}
	sh, err := s.sim.CreateShipment(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": sh.ID.String(), "ttn_id": sh.TTNID.String(), "number": sh.Number})
}

func (s *Server) cheque(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[sim.ChequeRequest](w, r)
	if !ok {
		return
	}
	reply, err := s.sim.Cheque(r.Context(), r.PathValue("fsrar"), s.publicURL, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

// checkPage — страница по QR-коду чека: покупатель видит, что продажа зафиксирована в ЕГАИС.
func (s *Server) checkPage(w http.ResponseWriter, r *http.Request) {
	fsrar, kind, marks, at, err := s.sim.ChequeInfo(r.Context(), r.PathValue("receipt"))
	if err != nil {
		http.Error(w, "Чек не найден в ЕГАИС имитатора", http.StatusNotFound)
		return
	}
	s.render(w, "check.html", map[string]any{"FSRAR": fsrar, "Return": kind == "return", "Marks": marks, "At": at})
}
