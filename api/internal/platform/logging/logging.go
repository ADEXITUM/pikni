// Package logging настраивает журнал приложения на стандартном пакете log/slog.
package logging

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// New пишет журнал в w, а если передан exporter — ещё и в него.
func New(w io.Writer, format string, level slog.Level, exporter slog.Handler) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var out slog.Handler
	if format == "text" {
		out = slog.NewTextHandler(w, opts)
	} else {
		out = slog.NewJSONHandler(w, opts)
	}

	var h slog.Handler = traceHandler{out}
	if exporter != nil {
		h = slog.NewMultiHandler(h, levelHandler{exporter, level})
	}
	return slog.New(h)
}

// traceHandler добавляет к записи trace_id и span_id текущей трассировки,
// чтобы строку журнала можно было найти по трассировке и наоборот.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

type levelHandler struct {
	slog.Handler
	min slog.Level
}

func (h levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.Handler.Enabled(ctx, l)
}

func (h levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelHandler{h.Handler.WithAttrs(attrs), h.min}
}

func (h levelHandler) WithGroup(name string) slog.Handler {
	return levelHandler{h.Handler.WithGroup(name), h.min}
}
