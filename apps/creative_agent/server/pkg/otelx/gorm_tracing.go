package otelx

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// GormTracing is a minimal GORM tracing plugin replacing gorm.io/plugin/opentelemetry (zero extra dependencies).
// It creates one client span per SQL statement (gorm.Create / gorm.Query / gorm.Update / gorm.Delete / gorm.Raw;
// db.Exec goes through the raw processor), records db.system / db.statement / db.rows_affected,
// and on error calls RecordError + sets Error status. The span hangs off db.Statement.Context,
// automatically forming a parent-child chain with the caller's ctx (the inbound HTTP span).
type GormTracing struct {
	provider trace.TracerProvider
}

// GormTracingOption configures GormTracing.
type GormTracingOption func(*GormTracing)

// WithTracerProvider specifies the TracerProvider (defaults to the global otel.GetTracerProvider()).
func WithTracerProvider(tp trace.TracerProvider) GormTracingOption {
	return func(p *GormTracing) { p.provider = tp }
}

// NewGormTracing creates the GORM tracing plugin.
func NewGormTracing(opts ...GormTracingOption) *GormTracing {
	p := &GormTracing{}
	for _, opt := range opts {
		opt(p)
	}
	if p.provider == nil {
		p.provider = otel.GetTracerProvider()
	}
	return p
}

// Name implements gorm.Plugin.
func (p *GormTracing) Name() string { return "otelx:gorm-tracing" }

// Initialize implements gorm.Plugin: registers before/after callbacks for the five kinds of SQL operations.
func (p *GormTracing) Initialize(db *gorm.DB) error {
	tracer := p.provider.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx")

	// addTracing attaches before/after callbacks to one kind of SQL operation:
	// before creates a span and puts it into Statement.Context; after collects attributes and ends it.
	// gorm's processor types are unexported, so the caller passes a Register closure with the Before/After anchors bound.
	addTracing := func(spanName string,
		registerBefore, registerAfter func(cbName string, fn func(*gorm.DB)) error,
	) error {
		before := func(db *gorm.DB) {
			if db.Statement == nil {
				return
			}
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx, _ = tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
			db.Statement.Context = ctx
		}
		after := func(db *gorm.DB) {
			if db.Statement == nil {
				return
			}
			span := trace.SpanFromContext(db.Statement.Context)
			span.SetAttributes(
				attribute.String("db.system", db.Dialector.Name()),
				attribute.String("db.statement", strings.TrimSpace(db.Statement.SQL.String())),
				attribute.Int64("db.rows_affected", db.RowsAffected),
			)
			if db.Error != nil {
				span.RecordError(db.Error)
				span.SetStatus(codes.Error, db.Error.Error())
			}
			span.End()
		}
		if err := registerBefore("otelx:tracing:before", before); err != nil {
			return err
		}
		return registerAfter("otelx:tracing:after", after)
	}

	if err := addTracing("gorm.Create",
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().Before("gorm:create").Register(n, f)
		},
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().After("gorm:create").Register(n, f)
		},
	); err != nil {
		return err
	}
	if err := addTracing("gorm.Query",
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().Before("gorm:query").Register(n, f)
		},
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().After("gorm:query").Register(n, f)
		},
	); err != nil {
		return err
	}
	if err := addTracing("gorm.Update",
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().Before("gorm:update").Register(n, f)
		},
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().After("gorm:update").Register(n, f)
		},
	); err != nil {
		return err
	}
	if err := addTracing("gorm.Delete",
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().Before("gorm:delete").Register(n, f)
		},
		func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().After("gorm:delete").Register(n, f)
		},
	); err != nil {
		return err
	}
	return addTracing("gorm.Raw",
		func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().Before("gorm:raw").Register(n, f) },
		func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().After("gorm:raw").Register(n, f) },
	)
}

// Compile-time assertion: GormTracing implements gorm.Plugin.
var _ gorm.Plugin = (*GormTracing)(nil)
