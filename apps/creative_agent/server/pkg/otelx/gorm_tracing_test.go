package otelx_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTracedDB 建一个挂了 GormTracing 插件的内存 sqlite DB（用内存 exporter 收 span）。
func newTracedDB(t *testing.T) (*gorm.DB, *sdktrace.TracerProvider, *otelx.MemoryExporter) {
	t.Helper()
	tp, exp := otelx.NewMemoryExporter()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.Use(otelx.NewGormTracing(otelx.WithTracerProvider(tp))))
	return db, tp, exp
}

// waitSpan 轮询等待满足条件的 span 出现。
func waitSpan(t *testing.T, exp *otelx.MemoryExporter, match func(s sdktrace.ReadOnlySpan) bool) sdktrace.ReadOnlySpan {
	t.Helper()
	var found sdktrace.ReadOnlySpan
	testx.Eventually(t, func() bool {
		for _, s := range exp.GetSpans() {
			if match(s) {
				found = s
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "应出现目标 GORM SQL span")
	return found
}

// TestGormSQLSpan 验证 GormTracing 为 SQL 建 span（取代 gorm.io/plugin/opentelemetry 的等价能力）。
func TestGormSQLSpan(t *testing.T) {
	db, tp, exp := newTracedDB(t)
	testx.NoError(t, db.Exec("CREATE TABLE t (id INTEGER)").Error)

	ctx, span := tp.Tracer("test").Start(context.Background(), "outer")
	testx.NoError(t, db.WithContext(ctx).Exec("INSERT INTO t VALUES (1)").Error)
	span.End()

	// db.Exec 走 raw processor：span 名 gorm.Raw（对齐原插件 gorm.Create/gorm.Exec/gorm.Raw 命名族）。
	// 按 SQL 过滤掉前面的 CREATE TABLE（也是 gorm.Raw）。
	s := waitSpan(t, exp, func(s sdktrace.ReadOnlySpan) bool {
		if s.Name() != "gorm.Raw" {
			return false
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "db.statement" && strings.Contains(kv.Value.AsString(), "INSERT") {
				return true
			}
		}
		return false
	})

	// 属性：db.system / db.statement / db.rows_affected。
	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		attrs[kv.Key] = kv.Value
	}
	testx.Equal(t, "sqlite", attrs["db.system"].AsString())
	testx.True(t, len(attrs["db.statement"].AsString()) > 0, "db.statement 应记录 SQL")
	testx.Equal(t, int64(1), attrs["db.rows_affected"].AsInt64())

	// 父子链：SQL span 应挂在 outer span 的同一条 trace 下。
	var outer sdktrace.ReadOnlySpan
	for _, sp := range exp.GetSpans() {
		if sp.Name() == "outer" {
			outer = sp
		}
	}
	testx.NotNil(t, outer)
	testx.Equal(t, outer.SpanContext().TraceID(), s.SpanContext().TraceID())
	testx.Equal(t, outer.SpanContext().SpanID(), s.Parent().SpanID())
}

// TestGormSQLSpanError 验证 SQL 出错时 span 记 Error status。
func TestGormSQLSpanError(t *testing.T) {
	db, _, exp := newTracedDB(t)

	err := db.Exec("INSERT INTO not_exist_table VALUES (1)").Error
	testx.NotNil(t, err)

	s := waitSpan(t, exp, func(s sdktrace.ReadOnlySpan) bool { return s.Name() == "gorm.Raw" })
	testx.Equal(t, "Error", s.Status().Code.String())
	testx.True(t, len(s.Events()) > 0, "应记录 exception 事件")
}

// TestGormCRUDSpans 验证 Create/Query/Update/Delete 四类操作各自建 span。
func TestGormCRUDSpans(t *testing.T) {
	db, _, exp := newTracedDB(t)

	type row struct {
		ID   uint `gorm:"primaryKey"`
		Name string
	}
	testx.NoError(t, db.AutoMigrate(&row{}))

	testx.NoError(t, db.Create(&row{Name: "a"}).Error)
	var got row
	testx.NoError(t, db.First(&got, 1).Error)
	testx.NoError(t, db.Model(&got).Update("name", "b").Error)
	testx.NoError(t, db.Delete(&got).Error)

	for _, want := range []string{"gorm.Create", "gorm.Query", "gorm.Update", "gorm.Delete"} {
		waitSpan(t, exp, func(s sdktrace.ReadOnlySpan) bool { return s.Name() == want })
	}
}
