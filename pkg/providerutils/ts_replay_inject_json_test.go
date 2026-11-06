package providerutils

import (
	"encoding/json"
	"strings"
	"testing"
)

// Replays packages/ai generate-object/inject-json-instruction.test.ts. Schemas
// are json.RawMessage so key order matches JSON.stringify of the TS literal.
func TestTSReplay_InjectJSONInstruction(t *testing.T) {
	basic := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"number"}},"required":["name","age"]}`)
	const must = "You MUST answer with a JSON object that matches the JSON schema above."
	var longProps []string
	for i := 0; i < 100; i++ {
		longProps = append(longProps, `"prop`+itoa(i)+`":{"type":"string"}`)
	}
	long := json.RawMessage(`{"type":"object","properties":{` + strings.Join(longProps, ",") + `}}`)
	nested := json.RawMessage(`{"type":"object","properties":{"person":{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"number"},"address":{"type":"object","properties":{"street":{"type":"string"},"city":{"type":"string"}}}}}}}`)
	special := json.RawMessage(`{"type":"object","properties":{"special@property":{"type":"string"},"emoji😊":{"type":"string"}}}`)

	cases := []struct {
		name   string
		prompt string
		schema interface{}
		opts   InjectJSONInstructionOptions
		want   string
	}{
		{"prompt and schema", "Generate a person", basic, InjectJSONInstructionOptions{}, "Generate a person\n\nJSON schema:\n" + string(basic) + "\n" + must},
		{"only prompt", "Generate a person", nil, InjectJSONInstructionOptions{}, "Generate a person\n\nYou MUST answer with JSON."},
		{"only schema", "", basic, InjectJSONInstructionOptions{}, "JSON schema:\n" + string(basic) + "\n" + must},
		{"nothing", "", nil, InjectJSONInstructionOptions{}, "You MUST answer with JSON."},
		{"custom prefix and suffix", "Generate a person", basic, InjectJSONInstructionOptions{SchemaPrefix: "Custom prefix:", SchemaSuffix: "Custom suffix"}, "Generate a person\n\nCustom prefix:\n" + string(basic) + "\nCustom suffix"},
		{"empty object schema", "Generate something", json.RawMessage(`{}`), InjectJSONInstructionOptions{}, "Generate something\n\nJSON schema:\n{}\n" + must},
		{"nested", "Generate a complex person", nested, InjectJSONInstructionOptions{}, "Generate a complex person\n\nJSON schema:\n" + string(nested) + "\n" + must},
		{"special characters", "", special, InjectJSONInstructionOptions{}, "JSON schema:\n" + string(special) + "\n" + must},
		{"long", strings.Repeat("A", 1000), long, InjectJSONInstructionOptions{}, strings.Repeat("A", 1000) + "\n\nJSON schema:\n" + string(long) + "\n" + must},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InjectJSONInstructionSchema(tc.prompt, tc.schema, tc.opts); got != tc.want {
				t.Fatalf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
