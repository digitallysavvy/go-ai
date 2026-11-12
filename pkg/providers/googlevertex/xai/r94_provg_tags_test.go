package xai

import (
	"reflect"
	"testing"
)

// r94provgUntagged lists the exported fields, at every depth, of the given
// response decode targets that have no json tag: case-exact decoding
// (cr-r94-carry-to-provg) fills a field only from its exact tag.
func r94provgUntagged(vs ...interface{}) []string {
	var out []string
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, path string)
	walk = func(t reflect.Type, path string) {
		for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		if _, custom := reflect.PointerTo(t).MethodByName("UnmarshalJSON"); custom && path != "" {
			return // a custom decoder is its own site (it decodes a tagged alias or raw struct)
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if tag == "" && !f.Anonymous {
				out = append(out, t.String()+"."+f.Name)
			}
			walk(f.Type, path+"."+f.Name)
		}
	}
	for _, v := range vs {
		walk(reflect.TypeOf(v), "")
	}
	return out
}

// TestR94provgResponseTargetsTagged guards the response decode targets
// classified R in round 94.
func TestR94provgResponseTargetsTagged(t *testing.T) {
	if bad := r94provgUntagged(vertexXaiUsage{}); len(bad) > 0 {
		t.Fatalf("untagged fields: %v", bad)
	}
}
