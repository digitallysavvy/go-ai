package moonshot

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// The TS assertion that the input schema is unchanged reads a variable the
// case evaluator does not hold; TestR90provbMFJSArrayChildrenOnce checks it.
func init() {
	tsfixture.RegisterPorted(map[string]string{
		tsfixture.PortedKey("normalizes existing prefixItems and tuple children in positional order", "schema.properties.row.items", "toEqual"): "TestR90provbMFJSArrayChildrenOnce",
	})
}

func r90provbJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func r90provbNormalized(t *testing.T, schema interface{}) interface{} {
	t.Helper()
	out, err := NormalizeJSONSchemaForMFJS(schema)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var v interface{}
	_ = json.Unmarshal(b, &v)
	return v
}

// TestR90provbMFJSArrayChildrenOnce ports "schema traversal" from
// normalize-json-schema-for-mfjs.test.ts (TS c480f3f726): nested array and
// tuple children are normalized once, with the same output, and existing
// prefixItems keep their position.
func TestR90provbMFJSArrayChildrenOnce(t *testing.T) {
	row := `{"type":"array","items":{"type":"object","properties":{"label":{"type":"string","anyOf":[{"minLength":1},{"const":""}]}}}}`
	normRow := `{"type":"array","items":{"type":"object","properties":{"label":{"anyOf":[{"type":"string","minLength":1},{"type":"string","const":""}]}}}}`
	for _, tuple := range []bool{false, true} {
		items, key := row, "items"
		if tuple {
			items, key = "["+row+"]", "prefixItems"
		}
		input := r90provbJSON(t, `{"type":"object","properties":{"rows":{"type":"array","items":`+items+`}}}`)
		want := `{"type":"object","properties":{"rows":{"type":"array","` + key + `":` + normRow + `}}}`
		if tuple {
			want = `{"type":"object","properties":{"rows":{"type":"array","prefixItems":[` + normRow + `]}}}`
		}
		if got := r90provbNormalized(t, input); !reflect.DeepEqual(got, r90provbJSON(t, want)) {
			t.Fatalf("tuple %v: got %v", tuple, got)
		}
	}

	schema := r90provbJSON(t, `{"type":"object","properties":{"row":{"type":"array","prefixItems":[{"type":"string","anyOf":[{"minLength":1}]},true],"items":[{"type":"array","items":[{"type":"number"},false]}]},"flags":{"type":"array","items":false}}}`)
	want := r90provbJSON(t, `{"type":"object","properties":{"row":{"type":"array","prefixItems":[{"anyOf":[{"type":"string","minLength":1}]},true,{"type":"array","prefixItems":[{"type":"number"},false]}]},"flags":{"type":"array","items":false}}}`)
	if got := r90provbNormalized(t, schema); !reflect.DeepEqual(got, want) {
		t.Fatalf("positional order: got %v", got)
	}
	rowItems := schema.(map[string]interface{})["properties"].(map[string]interface{})["row"].(map[string]interface{})["items"]
	if !reflect.DeepEqual(rowItems, r90provbJSON(t, `[{"type":"array","items":[{"type":"number"},false]}]`)) {
		t.Fatalf("input schema changed: %v", rowItems)
	}
}

// TestR90provbMFJSDeepArraysLinear: each array level is visited once, so a
// deeply nested array schema normalizes at once (visiting items twice per
// level took 2^depth steps).
func TestR90provbMFJSDeepArraysLinear(t *testing.T) {
	inner := `{"type":"object","properties":{"x":{"type":"string"}}}`
	for i := 0; i < 40; i++ {
		inner = `{"type":"array","items":` + inner + `}`
	}
	schema := r90provbJSON(t, `{"type":"object","properties":{"a":`+inner+`}}`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = NormalizeJSONSchemaForMFJS(schema)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("normalizing 40 nested arrays did not finish")
	}
}
