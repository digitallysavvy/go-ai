package schema

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func r82integSchemaJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(NewSimpleStructSchema(reflect.TypeOf(v)))
	if err != nil {
		t.Fatal(err)
	}
	// JSONSchema() is the same schema as a map.
	var fromMarshal, fromMap interface{}
	_ = json.Unmarshal(b, &fromMarshal)
	mb, _ := json.Marshal(NewStructSchema(reflect.TypeOf(v)).JSONSchema())
	_ = json.Unmarshal(mb, &fromMap)
	if !reflect.DeepEqual(fromMarshal, fromMap) {
		t.Fatalf("MarshalJSON %s and JSONSchema() %s differ", b, mb)
	}
	return string(b)
}

// TS zodSchema(z.object(...)) snapshots (provider-utils schema.test.ts and
// __snapshots__/schema.test.ts.snap, zod v4) with the Go struct twin of each
// zod object; keys in zod's emit order.
func TestR82integStructSchemaMatchesZodSnapshots(t *testing.T) {
	const s7 = `{"$schema":"http://json-schema.org/draft-07/schema#",`
	type category struct {
		Name          string     `json:"name" validate:"required"`
		Subcategories []category `json:"subcategories" validate:"required"`
	}
	cases := []struct {
		name string
		v    interface{}
		want string
	}{
		{"should create a schema with simple types", struct {
			Text   string  `json:"text" validate:"required"`
			Number float64 `json:"number" validate:"required"`
		}{}, s7 + `"type":"object","properties":{"text":{"type":"string"},"number":{"type":"number"}},"required":["text","number"],"additionalProperties":false}`},
		{"should support optional fields in object", struct {
			Required string `json:"required" validate:"required"`
			Optional string `json:"optional,omitempty"`
		}{}, s7 + `"type":"object","properties":{"required":{"type":"string"},"optional":{"type":"string"}},"required":["required"],"additionalProperties":false}`},
		{"should support optional fields with descriptions in object", struct {
			Required string `json:"required" validate:"required" jsonschema:"description=Required description"`
			Optional string `json:"optional,omitempty" description:"Optional description"`
		}{}, s7 + `"type":"object","properties":{"required":{"type":"string","description":"Required description"},"optional":{"type":"string","description":"Optional description"}},"required":["required"],"additionalProperties":false}`},
		{"should support arrays", struct {
			Items []string `json:"items" validate:"required"`
		}{}, s7 + `"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"required":["items"],"additionalProperties":false}`},
		{"should support optional arrays", struct {
			Items []string `json:"items,omitempty"`
		}{}, s7 + `"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`},
		{"should support required enums", struct {
			Type string `json:"type" validate:"required,oneof=a b c"`
		}{}, s7 + `"type":"object","properties":{"type":{"type":"string","enum":["a","b","c"]}},"required":["type"],"additionalProperties":false}`},
		{"should support optional enums", struct {
			Type string `json:"type,omitempty" validate:"omitempty,oneof=a b c"`
		}{}, s7 + `"type":"object","properties":{"type":{"type":"string","enum":["a","b","c"]}},"additionalProperties":false}`},
		{"should support nullable", struct {
			Location *string `json:"location" jsonschema:"required"`
		}{}, s7 + `"type":"object","properties":{"location":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["location"],"additionalProperties":false}`},
		{"should generate JSON schema for input when transform is used", struct {
			User struct {
				ID   string `json:"id" validate:"required"`
				Name string `json:"name" validate:"required"`
			} `json:"user" validate:"required"`
		}{}, s7 + `"type":"object","properties":{"user":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"additionalProperties":false}},"required":["user"],"additionalProperties":false}`},
		{"should preserve record value schemas", struct {
			Values map[string]string `json:"values" validate:"required"`
		}{}, s7 + `"type":"object","properties":{"values":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}}},"required":["values"],"additionalProperties":false}`},
		{"should use recursive references with z.lazy", struct {
			Category category `json:"category" validate:"required"`
		}{}, s7 + `"type":"object","properties":{"category":{"$ref":"#/definitions/__schema0"}},"required":["category"],"definitions":{"__schema0":{"type":"object","properties":{"name":{"type":"string"},"subcategories":{"type":"array","items":{"$ref":"#/definitions/__schema0"}}},"required":["name","subcategories"],"additionalProperties":false}},"additionalProperties":false}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r82integSchemaJSON(t, c.v); got != c.want {
				t.Fatalf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

type r82integEmbedded struct {
	Shared string `json:"shared"`
	Hidden string `json:"-"`
}

type r82integNode struct {
	Value    int             `json:"value" validate:"required"`
	Children []*r82integNode `json:"children,omitempty"`
}

// The Go field mapping: json names, "-" and unexported fields skipped,
// embedded structs promoted, pointers optional and nullable, Go kinds,
// time.Time, []byte, maps, validate ranges, root recursion as "#".
func TestR82integStructSchemaGoMapping(t *testing.T) {
	type all struct {
		r82integEmbedded
		Name     string          `json:"name" validate:"required,min=2,max=5"`
		Nick     *string         `json:"nick"`
		Age      uint8           `json:"age" validate:"gte=18"`
		Score    float64         `json:"score" validate:"gt=0,lte=1"`
		Count    int             `json:"count,string"`
		Tags     []string        `json:"tags" validate:"min=1"`
		Blob     []byte          `json:"blob"`
		At       time.Time       `json:"at"`
		Meta     map[string]int  `json:"meta"`
		ByID     map[int]bool    `json:"byId"`
		Any      interface{}     `json:"any"`
		Raw      json.RawMessage `json:"raw"`
		Color    string          `json:"color" validate:"oneof='light red' blue"`
		Level    int32           `json:"level" validate:"oneof=1 2 3"`
		NoTag    bool
		Fn       func()            `json:"fn"`
		private  string            //nolint:unused // exercises the unexported skip
		Children []*r82integNode   `json:"children" validate:"required"`
		Ptr      *int              `json:"ptr" validate:"required"`
		Labels   map[string]string `json:"labels,omitempty" validate:"max=3"`
	}
	const want = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{` +
		`"shared":{"type":"string"},` +
		`"name":{"type":"string","minLength":2,"maxLength":5},` +
		`"nick":{"anyOf":[{"type":"string"},{"type":"null"}]},` +
		`"age":{"type":"integer","minimum":18,"maximum":255},` +
		`"score":{"type":"number","maximum":1,"exclusiveMinimum":0},` +
		`"count":{"type":"string"},` +
		`"tags":{"type":"array","items":{"type":"string"},"minItems":1},` +
		`"blob":{"type":"string"},` +
		`"at":{"type":"string","format":"date-time"},` +
		`"meta":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"integer"}},` +
		`"byId":{"type":"object","propertyNames":{"type":"string","pattern":"^-?[0-9]+$"},"additionalProperties":{"type":"boolean"}},` +
		`"any":{},"raw":{},` +
		`"color":{"type":"string","enum":["light red","blue"]},` +
		`"level":{"type":"integer","minimum":-2147483648,"maximum":2147483647,"enum":[1,2,3]},` +
		`"NoTag":{"type":"boolean"},` +
		`"children":{"type":"array","items":{"$ref":"#/definitions/__schema0"}},` +
		`"ptr":{"type":"integer"},` +
		`"labels":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"},"maxProperties":3}},` +
		`"required":["name","children","ptr"],` +
		`"definitions":{"__schema0":{"type":"object","properties":{"value":{"type":"integer"},"children":{"type":"array","items":{"$ref":"#/definitions/__schema0"}}},"required":["value"],"additionalProperties":false}},` +
		`"additionalProperties":false}`
	_ = all{}.private
	if got := r82integSchemaJSON(t, all{}); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	const node = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"type":"integer"},"children":{"type":"array","items":{"$ref":"#"}}},"required":["value"],"additionalProperties":false}`
	if got := r82integSchemaJSON(t, r82integNode{}); got != node {
		t.Fatalf("root recursion: got %s\nwant %s", got, node)
	}
	if got := r82integSchemaJSON(t, &r82integNode{}); got != node {
		t.Fatalf("pointer root: got %s", got)
	}
}

type r82integAgree struct {
	Name   string            `json:"name" validate:"required,min=2,max=5"`
	Kind   string            `json:"kind" validate:"omitempty,oneof=a b"`
	Nick   *string           `json:"nick"`
	Must   *string           `json:"must" jsonschema:"required"`
	Small  int8              `json:"small"`
	Count  uint              `json:"count" validate:"lte=10"`
	Tags   []string          `json:"tags" validate:"max=2"`
	At     time.Time         `json:"at"`
	Meta   map[string]int    `json:"meta"`
	Inner  *r82integAgreeIn  `json:"inner"`
	Tree   *r82integNode     `json:"tree"`
	Labels map[string]string `json:"labels"`
}

type r82integAgreeIn struct {
	ID string `json:"id" validate:"required"`
}

// Validate accepts exactly the decoded values the JSON Schema accepts, for
// inputs without unknown keys (zod strips those, so Validate ignores them).
func TestR82integStructValidateAgreesWithSchema(t *testing.T) {
	v := NewStructSchema(reflect.TypeOf(r82integAgree{}))
	wire := v.JSONSchema()
	base := func() map[string]interface{} {
		return map[string]interface{}{"name": "abc", "must": nil}
	}
	with := func(k string, val interface{}) map[string]interface{} {
		m := base()
		m[k] = val
		return m
	}
	without := func(k string) map[string]interface{} {
		m := base()
		delete(m, k)
		return m
	}
	inputs := map[string]map[string]interface{}{
		"minimal":                base(),
		"missing required name":  without("name"),
		"missing required must":  without("must"),
		"name too short":         with("name", "a"),
		"name too long":          with("name", "abcdef"),
		"name null":              with("name", nil),
		"name number":            with("name", 3.0),
		"kind in enum":           with("kind", "a"),
		"kind not in enum":       with("kind", "c"),
		"nick string":            with("nick", "n"),
		"nick null":              with("nick", nil),
		"nick number":            with("nick", 1.0),
		"must string":            with("must", "m"),
		"small in range":         with("small", 127.0),
		"small out of range":     with("small", 128.0),
		"small fraction":         with("small", 1.5),
		"count negative":         with("count", -1.0),
		"count over max":         with("count", 11.0),
		"count ok":               with("count", 10.0),
		"tags ok":                with("tags", []interface{}{"x", "y"}),
		"tags too many":          with("tags", []interface{}{"x", "y", "z"}),
		"tags wrong item":        with("tags", []interface{}{1.0}),
		"at date-time":           with("at", "2026-11-02T10:00:00Z"),
		"at not a date":          with("at", "yesterday"),
		"meta ok":                with("meta", map[string]interface{}{"a": 1.0}),
		"meta wrong value":       with("meta", map[string]interface{}{"a": "x"}),
		"inner ok":               with("inner", map[string]interface{}{"id": "i"}),
		"inner missing id":       with("inner", map[string]interface{}{}),
		"inner null":             with("inner", nil),
		"tree nested ok":         with("tree", map[string]interface{}{"value": 1.0, "children": []interface{}{map[string]interface{}{"value": 2.0}}}),
		"tree nested bad":        with("tree", map[string]interface{}{"value": 1.0, "children": []interface{}{map[string]interface{}{}}}),
		"labels ok":              with("labels", map[string]interface{}{"k": "v"}),
		"labels wrong value":     with("labels", map[string]interface{}{"k": true}),
		"name and kind together": func() map[string]interface{} { m := with("kind", "b"); m["name"] = "xy"; return m }(),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			schemaErr := validateJSONSchemaValue(in, wire, "$")
			validateErr := v.Validate(in)
			if (schemaErr == nil) != (validateErr == nil) {
				t.Fatalf("schema: %v\nValidate: %v", schemaErr, validateErr)
			}
		})
	}
}

type r82integRec struct {
	Name *string       `json:"name" jsonschema:"required"`
	Kids []r82integRec `json:"kids"`
}

// A root-recursive struct ({"$ref": "#"}) is validated at every depth.
func TestR82integStructValidateRootRecursion(t *testing.T) {
	v := NewStructSchema(reflect.TypeOf(r82integRec{}))
	if err := v.Validate(map[string]interface{}{"name": nil, "kids": []interface{}{map[string]interface{}{"name": "k"}}}); err != nil {
		t.Fatalf("valid tree: %v", err)
	}
	if err := v.Validate(map[string]interface{}{"name": nil, "kids": []interface{}{map[string]interface{}{}}}); err == nil {
		t.Fatal("nested kid without the required name: want an error")
	}
}
