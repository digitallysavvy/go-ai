package schema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type r83genZodIn struct {
	ID string `json:"id" validate:"required"`
}

type r83genZodT struct {
	Obj    r83genZodIn  `json:"obj" validate:"required" description:"o"`
	OptObj r83genZodIn  `json:"optObj,omitempty" description:"p"`
	Nick   *string      `json:"nick" description:"n"`
	Must   *string      `json:"must" jsonschema:"required,description=m"`
	Count  int          `json:"count" validate:"required"`
	Size   uint         `json:"size,omitempty"`
	Score  float64      `json:"score" validate:"required,gte=1,gt=0"`
	Ratio  float64      `json:"ratio" validate:"required,gte=0,max=5,lt=3"`
	Tags   []string     `json:"tags,omitempty" validate:"omitempty,min=1,max=3" description:"t"`
	Any    *interface{} `json:"any"`
}

// The zod v4 key order (toJSONSchema draft-7, io input, then
// addAdditionalPropertiesToJsonSchema, as TS zodSchema) of the zod twin of
// r83genZodT, printed by zod 4.4.3:
//
//	z.object({obj: Inner.describe('o'), optObj: Inner.optional().describe('p'),
//	  nick: z.string().nullish().describe('n'), must: z.string().nullable().describe('m'),
//	  count: z.number().int(), size: z.number().int().nonnegative().optional(),
//	  score: z.number().min(1).gt(0), ratio: z.number().gte(0).max(5).lt(3),
//	  tags: z.array(z.string()).min(1).max(3).optional().describe('t'),
//	  any: z.any().nullish()})
//
// A description goes first on an optional field and before the appended
// additionalProperties false of an object; array bounds precede "type"; only
// the tighter of minimum/exclusiveMinimum is written; every integer has zod
// .int()'s safe range.
func TestR83genStructSchemaZodKeyOrder(t *testing.T) {
	const want = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{` +
		`"obj":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"description":"o","additionalProperties":false},` +
		`"optObj":{"description":"p","type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false},` +
		`"nick":{"description":"n","anyOf":[{"type":"string"},{"type":"null"}]},` +
		`"must":{"anyOf":[{"type":"string"},{"type":"null"}],"description":"m"},` +
		`"count":{"type":"integer","minimum":-9007199254740991,"maximum":9007199254740991},` +
		`"size":{"type":"integer","minimum":0,"maximum":9007199254740991},` +
		`"score":{"type":"number","minimum":1},` +
		`"ratio":{"type":"number","minimum":0,"exclusiveMaximum":3},` +
		`"tags":{"description":"t","minItems":1,"maxItems":3,"type":"array","items":{"type":"string"}},` +
		`"any":{"anyOf":[{},{"type":"null"}]}},` +
		`"required":["obj","must","count","score","ratio"],"additionalProperties":false}`
	if got := r82integSchemaJSON(t, r83genZodT{}); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

type r83genSelf struct {
	V    string      `json:"v" validate:"required"`
	Self *r83genSelf `json:"self" validate:"required" description:"s"`
}

// A described required reference keeps its description out of the $ref's
// siblings (draft-07 ignores them): zod emits {"description", "allOf": [ref]}.
func TestR83genStructSchemaDescribedRef(t *testing.T) {
	got := r82integSchemaJSON(t, r83genSelf{})
	if !strings.Contains(got, `"self":{"description":"s","allOf":[{"$ref":"#"}]}`) {
		t.Fatalf("got %s", got)
	}
}

// zod .int() rejects an integer beyond Number.MAX_SAFE_INTEGER; Validate
// agrees with the schema it sends.
func TestR83genStructValidateSafeIntegerRange(t *testing.T) {
	type v struct {
		N int64  `json:"n"`
		U uint64 `json:"u"`
	}
	sv := NewStructSchema(reflect.TypeOf(v{}))
	if err := sv.Validate(map[string]interface{}{"n": float64(-9007199254740991), "u": float64(9007199254740991)}); err != nil {
		t.Fatalf("safe bounds rejected: %v", err)
	}
	for _, in := range []map[string]interface{}{
		{"n": float64(9007199254740994)},
		{"n": float64(-9007199254740994)},
		{"u": float64(9007199254740994)},
	} {
		if err := sv.Validate(in); err == nil {
			t.Fatalf("%v accepted", in)
		}
	}
}

// encoding/json isValidTag: a tag name with a non-letter symbol outside the
// allowed punctuation is ignored and the Go field name is used.
func TestR83genStructSchemaInvalidTagName(t *testing.T) {
	type v struct {
		Price int `json:"price€"`
		Café  int `json:"café"`
	}
	got := r82integSchemaJSON(t, v{})
	if !strings.Contains(got, `"Price":`) || !strings.Contains(got, `"café":`) {
		t.Fatalf("got %s", got)
	}
}

// A zero or nil JSON schema value is no schema; marshaling it writes {}
// instead of dereferencing the nil validator (carry CR r83 item 1).
func TestR83genNilJSONSchemaMarshal(t *testing.T) {
	for name, f := range map[string]func() ([]byte, error){
		"zero SimpleJSONSchema":   func() ([]byte, error) { return json.Marshal(&SimpleJSONSchema{}) },
		"nil SimpleJSONSchema":    func() ([]byte, error) { return (*SimpleJSONSchema)(nil).MarshalJSON() },
		"nil JSONSchemaValidator": func() ([]byte, error) { return (*JSONSchemaValidator)(nil).MarshalJSON() },
	} {
		b, err := f()
		if err != nil || string(b) != "{}" {
			t.Fatalf("%s: %s, %v", name, b, err)
		}
	}
}
