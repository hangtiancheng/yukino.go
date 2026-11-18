package tools

import (
	"testing"
)

func TestMysqlCrudToolSchema(t *testing.T) {
	tool, err := NewMysqlCrudTool()
	if err != nil {
		t.Fatalf("NewMysqlCrudTool: %v", err)
	}
	js := tool.Schema()["input_schema"].(map[string]any)
	properties := js["properties"].(map[string]any)
	prop := func(name string) (string, []any) {
		t.Helper()
		value, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		desc, _ := value["description"].(string)
		enum, _ := value["enum"].([]any)
		return desc, enum
	}

	dsnDesc, _ := prop("dsn")
	wantDsnDesc := "MySQL DSN, including username/password/host/port/database name, e.g., root:pass@tcp(host:3306)/db"
	if dsnDesc != wantDsnDesc {
		t.Errorf("dsn description = %q, want %q", dsnDesc, wantDsnDesc)
	}

	sqlDesc, _ := prop("sql")
	if sqlDesc != "SQL statement to execute" {
		t.Errorf("sql description = %q", sqlDesc)
	}

	opDesc, opEnum := prop("operate_type")
	if opDesc != "SQL operation type" {
		t.Errorf("operate_type description = %q", opDesc)
	}
	wantEnum := []any{"query", "insert", "update", "delete"}
	if len(opEnum) != len(wantEnum) {
		t.Fatalf("operate_type enum = %v, want %v", opEnum, wantEnum)
	}
	for i, v := range wantEnum {
		if opEnum[i] != v {
			t.Errorf("operate_type enum[%d] = %v, want %v", i, opEnum[i], v)
		}
	}

	for _, name := range []string{"dsn", "sql", "operate_type"} {
		found := false
		for _, v := range js["required"].([]any) {
			if v == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%s should be required (required=%v)", name, js["required"])
		}
	}
}

func TestNormalizeDsn(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"root:pass@tcp(127.0.0.1:3306)/db", "root:pass@tcp(127.0.0.1:3306)/db?parseTime=true"},
		{"mysql://root:pass@127.0.0.1:3306/db", "root:pass@tcp(127.0.0.1:3306)/db?parseTime=true"},
		{"mysql://root:pass@127.0.0.1:3306/", "root:pass@tcp(127.0.0.1:3306)/?parseTime=true"},
		{"root:pass@tcp(h:3306)/db?parseTime=true", "root:pass@tcp(h:3306)/db?parseTime=true"},
		{"root:pass@tcp(h:3306)/db?timeout=5s", "root:pass@tcp(h:3306)/db?timeout=5s&parseTime=true"},
	}
	for _, c := range cases {
		if got := normalizeDsn(c.in); got != c.want {
			t.Errorf("normalizeDsn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
