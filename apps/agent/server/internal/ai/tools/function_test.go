package tools

import (
	"context"
	"strings"
	"testing"

	sdktools "github.com/hangtiancheng/yukino-code/apps/yukino/tools"
)

func TestNativeToolValidationAndCancellation(t *testing.T) {
	tool, err := NewMysqlCrudTool()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{nil, {"dsn": "test", "sql": "select 1", "operate_type": "drop"}, {"dsn": 12, "sql": "select 1", "operate_type": "query"}} {
		result := tool.Execute(context.Background(), args)
		if !result.IsError {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
	timeTool, err := NewGetCurrentTimeTool()
	if err != nil {
		t.Fatal(err)
	}
	if result := timeTool.Execute(context.Background(), nil); result.IsError || !strings.Contains(result.Output, "timestamp") {
		t.Fatalf("empty arguments: %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := timeTool.Execute(ctx, nil); !result.IsError {
		t.Fatal("ignored cancellation")
	}
	called := false
	custom, err := newFunction("cancel_test", "test", sdktools.CategoryRead, func(context.Context, *GetCurrentTimeInput) (string, error) { called = true; return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	custom.Execute(ctx, nil)
	if called {
		t.Fatal("executed cancelled tool")
	}
}
