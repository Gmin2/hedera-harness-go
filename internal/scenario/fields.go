package scenario

import (
	"reflect"
	"strings"
)

// Fields lists the yaml field names a step struct accepts, following inline
// embeds, so help output and agent prompts never drift from the code.
func Fields(v any) []string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []string
	collectFields(t, &out)
	return out
}

func collectFields(t reflect.Type, out *[]string) {
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		if strings.Contains(opts, "inline") {
			collectFields(f.Type, out)
			continue
		}
		if name == "" {
			continue
		}
		*out = append(*out, name)
	}
}
