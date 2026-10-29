package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TestOTelTracing verifies that the OTel middleware injects a span-carrying ctx into the request.
func TestOTelTracing(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", tp))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.True(t, sc.IsValid(), "the request ctx should carry a valid span")
		testx.True(t, sc.IsSampled(), "the span should be sampled (AlwaysSample)")
		c.SetStatus(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)

	// The RequestID middleware generates a uuid; the OTel middleware should write it into the span attributes (http.request_id).
	testx.Eventually(t, func() bool {
		for _, s := range exp.GetSpans() {
			if s.Name() == "HTTP GET /ping" {
				for _, kv := range s.Attributes() {
					if kv.Key == "http.request_id" && kv.Value.AsString() != "" {
						return true
					}
				}
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "the span should carry the http.request_id attribute")
}

// TestOTelTracingTraceParent verifies that when an inbound request carries a traceparent header,
// the middleware Extracts the remote parent span and creates a child span (same TraceID → same trace).
func TestOTelTracingTraceParent(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Production path: otelx.Init sets the global W3C propagator when enabled; the middleware reads it via otel.GetTextMapPropagator.
	// NewMemoryExporter deliberately does not touch globals (test isolation), so set it manually here and restore with defer.
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)
	defer otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", tp))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.True(t, sc.IsValid(), "the request ctx should carry a valid span")
		testx.True(t, sc.IsSampled(), "the span should be sampled (AlwaysSample)")
		c.SetStatus(200)
	})

	// Build a remote parent span ctx with a known TraceID and serialize it into a traceparent header with the global W3C propagator.
	parentTraceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	parentSpanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parentSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    parentTraceID,
		SpanID:     parentSpanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	parentCtx := trace.ContextWithRemoteSpanContext(context.Background(), parentSC)

	carrier := propagation.HeaderCarrier(http.Header{})
	propagator.Inject(parentCtx, carrier)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("traceparent", carrier.Get("traceparent"))
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)

	// The child span should inherit the parent TraceID (same trace) and carry the request_id attribute.
	testx.Eventually(t, func() bool {
		for _, s := range exp.GetSpans() {
			if s.Name() == "HTTP GET /ping" {
				if s.SpanContext().TraceID() != parentTraceID {
					return false
				}
				for _, kv := range s.Attributes() {
					if kv.Key == "http.request_id" && kv.Value.AsString() != "" {
						return true
					}
				}
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "the child span should inherit the parent TraceID and carry the request_id attribute")
}

// TestOTelTracingNilProvider verifies that when TracerProvider is nil (OTel disabled)
// the middleware passes through without creating a span or panicking.
func TestOTelTracingNilProvider(t *testing.T) {
	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", nil))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.False(t, sc.IsValid(), "the ctx should carry no span when the provider is nil")
		c.SetStatus(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)
}
