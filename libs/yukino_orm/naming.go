package yukino_orm

import (
	"reflect"
	"strings"
)

func CollectionName(value any) string {
	typ := reflect.TypeOf(value)
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil {
		return ""
	}
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
	}
	if typ.Kind() != reflect.Struct {
		return ""
	}
	return pluralize(toSnake(typ.Name()))
}

func toSnake(value string) string {
	var out strings.Builder
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}

func pluralize(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, "y") && len(name) >= 2 && !isVowel(name[len(name)-2]) {
		return strings.TrimSuffix(name, "y") + "ies"
	}
	if strings.HasSuffix(name, "s") || strings.HasSuffix(name, "x") || strings.HasSuffix(name, "sh") || strings.HasSuffix(name, "ch") {
		return name + "es"
	}
	return name + "s"
}

func isVowel(c byte) bool {
	return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u'
}
