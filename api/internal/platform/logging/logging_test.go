package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLogLineHasTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "json", slog.LevelInfo, nil)

	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()
	log.InfoContext(ctx, "hello")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if line["trace_id"] != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id = %v, want %s", line["trace_id"], span.SpanContext().TraceID())
	}
	if line["span_id"] != span.SpanContext().SpanID().String() {
		t.Errorf("span_id = %v, want %s", line["span_id"], span.SpanContext().SpanID())
	}
}

func TestExporterRespectsLevel(t *testing.T) {
	var out, exported bytes.Buffer
	log := New(&out, "json", slog.LevelWarn, slog.NewJSONHandler(&exported, &slog.HandlerOptions{Level: slog.LevelDebug}))

	log.Info("skipped")
	log.Warn("kept")

	if bytes.Contains(exported.Bytes(), []byte("skipped")) {
		t.Error("exporter got a record below the configured level")
	}
	if !bytes.Contains(exported.Bytes(), []byte("kept")) || !bytes.Contains(out.Bytes(), []byte("kept")) {
		t.Error("warn record must reach both stdout and exporter")
	}
}
