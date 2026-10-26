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

// GormTracing 是极简的 GORM tracing 插件，取代 gorm.io/plugin/opentelemetry（零额外依赖）。
// 每条 SQL 建一个 client span（gorm.Create / gorm.Query / gorm.Update / gorm.Delete / gorm.Raw，
// db.Exec 走 raw processor），记录 db.system / db.statement / db.rows_affected，
// 出错时 RecordError + Error status。span 挂在 db.Statement.Context 上，
// 与调用方 ctx（HTTP 入站 span）自动形成父子链。
type GormTracing struct {
	provider trace.TracerProvider
}

// GormTracingOption 配置 GormTracing。
type GormTracingOption func(*GormTracing)

// WithTracerProvider 指定 TracerProvider（默认全局 otel.GetTracerProvider()）。
func WithTracerProvider(tp trace.TracerProvider) GormTracingOption {
	return func(p *GormTracing) { p.provider = tp }
}

// NewGormTracing 创建 GORM tracing 插件。
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

// Name 实现 gorm.Plugin。
func (p *GormTracing) Name() string { return "otelx:gorm-tracing" }

// Initialize 实现 gorm.Plugin：为五类 SQL 操作注册 before/after 回调。
func (p *GormTracing) Initialize(db *gorm.DB) error {
	tracer := p.provider.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx")

	// addTracing 给一类 SQL 操作挂 before/after 回调：
	// before 建 span 塞进 Statement.Context，after 收属性并 End。
	// gorm 的 processor 是未导出类型，由调用方传入绑定好 Before/After 锚点的 Register 闭包。
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

// 编译期断言：GormTracing 实现 gorm.Plugin。
var _ gorm.Plugin = (*GormTracing)(nil)
