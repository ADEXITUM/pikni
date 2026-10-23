// Package telemetry подключает OpenTelemetry: трассировки, метрики и журналы уходят по OTLP/HTTP.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Endpoint       string
}

type Telemetry struct {
	// LogHandler отправляет записи журнала в OpenTelemetry; nil, если телеметрия выключена.
	LogHandler slog.Handler
	shutdown   []func(context.Context) error
}

// Setup без адреса OTLP ничего не отправляет: тесты и CI работают без коллектора.
// Адрес и остальные параметры экспортёры читают из переменных OTEL_EXPORTER_OTLP_*.
func Setup(ctx context.Context, cfg Config) (*Telemetry, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	t := &Telemetry{}
	if cfg.Endpoint == "" {
		return t, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	t.shutdown = append(t.shutdown, tp.Shutdown)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("metric exporter: %w", err), t.Shutdown(ctx))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	t.shutdown = append(t.shutdown, mp.Shutdown)

	if err := runtime.Start(); err != nil {
		return nil, errors.Join(fmt.Errorf("runtime metrics: %w", err), t.Shutdown(ctx))
	}

	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("log exporter: %w", err), t.Shutdown(ctx))
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	otel.SetLoggerProvider(lp)
	t.shutdown = append(t.shutdown, lp.Shutdown)
	t.LogHandler = otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp))

	return t, nil
}

func (t *Telemetry) Enabled() bool {
	return len(t.shutdown) > 0
}

// Shutdown отправляет накопленные данные и закрывает экспортёры.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for i := len(t.shutdown) - 1; i >= 0; i-- {
		errs = append(errs, t.shutdown[i](ctx))
	}
	t.shutdown = nil
	return errors.Join(errs...)
}
