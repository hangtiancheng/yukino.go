package milvus

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestEscapeMilvusString(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain.md`, `plain.md`},
		{`with"quote.md`, `with\"quote.md`},
		{`with\backslash.md`, `with\\backslash.md`},
		{`"both"\`, `\"both\"\\`},
	}
	for _, c := range cases {
		if got := escapeMilvusString(c.in); got != c.want {
			t.Errorf("escapeMilvusString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVectorDim(t *testing.T) {
	schema := entity.NewSchema().WithName("biz")
	for _, f := range Fields(2048) {
		schema.WithField(f)
	}

	dim, ok := vectorDim(schema)
	if !ok {
		t.Fatal("vectorDim failed on schema built from Fields()")
	}
	if dim != 2048 {
		t.Errorf("dim = %d, want 2048", dim)
	}

	empty := entity.NewSchema().WithName("empty").
		WithField(entity.NewField().WithName("other").WithDataType(entity.FieldTypeVarChar).WithMaxLength(8))
	if _, ok := vectorDim(empty); ok {
		t.Error("want ok=false when the vector field is missing")
	}
	if _, ok := vectorDim(nil); ok {
		t.Error("want ok=false for nil schema")
	}
}

func TestFieldsSchema(t *testing.T) {
	fields := Fields(1024)
	if len(fields) != 4 {
		t.Fatalf("want 4 fields, got %d", len(fields))
	}
	byName := map[string]*entity.Field{}
	for _, f := range fields {
		byName[f.Name] = f
	}

	id := byName[consts.MilvusIDField]
	if id == nil || !id.PrimaryKey || id.DataType != entity.FieldTypeVarChar {
		t.Errorf("id field = %+v, want VarChar primary key", id)
	}
	vec := byName[consts.MilvusVectorField]
	if vec == nil || vec.DataType != entity.FieldTypeFloatVector {
		t.Fatalf("vector field = %+v, want FloatVector", vec)
	}
	if vec.TypeParams["dim"] != "1024" {
		t.Errorf("vector dim = %q, want 1024", vec.TypeParams["dim"])
	}
	content := byName[consts.MilvusContentField]
	if content == nil || content.DataType != entity.FieldTypeVarChar ||
		content.TypeParams["max_length"] != "8192" {
		t.Errorf("content field = %+v, want VarChar(8192)", content)
	}
	meta := byName[consts.MilvusMetadataField]
	if meta == nil || meta.DataType != entity.FieldTypeJSON {
		t.Errorf("metadata field = %+v, want JSON", meta)
	}
}
