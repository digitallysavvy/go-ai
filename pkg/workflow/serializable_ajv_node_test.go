package workflow

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestValidateSerializableToolInputMatchesAjv replays (schema, data) pairs
// through Ajv 8.11 (allErrors, strict false, validateFormats false, Ajv2020
// for a 2020-12 $schema) and the TS formatAjvError, and compares the error
// text. testdata/ajv_cases.json.gz holds schemas and data in draft-07 and
// 2020-12 covering the keyword groups, boolean schemas, $ref, numeric
// limits, and the contains/uniqueItems/propertyNames/dependencies/unevaluated
// families.
//
// Go object input is a map, so Ajv's insertion-order walk over unexpected
// properties cannot be reproduced; those cases compare the same set of
// messages (the order of additionalProperties issues is a known leniency).
// Patterns that need lookahead, lookbehind or backreferences cannot compile
// in RE2 and are not in the file.
func TestValidateSerializableToolInputMatchesAjv(t *testing.T) {
	f, err := os.Open("testdata/ajv_cases.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Schema json.RawMessage `json:"schema"`
		Data   interface{}     `json:"data"`
		Msg    *string         `json:"msg"`
	}
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 5000 {
		t.Fatalf("fixture has %d cases", len(cases))
	}
	sorted := func(msg string) string {
		parts := strings.Split(msg, ", ")
		sort.Strings(parts)
		return strings.Join(parts, ", ")
	}
	reported := 0
	for _, c := range cases {
		var def SerializableToolDef
		if err := json.Unmarshal([]byte(`{"name":"w","inputSchema":`+string(c.Schema)+`}`), &def); err != nil {
			t.Fatal(err)
		}
		_, err := ValidateSerializableToolInput(def, c.Data)
		got, want := "", ""
		if err != nil {
			got = strings.TrimPrefix(err.Error(), "workflow: invalid input for w: ")
		}
		if c.Msg != nil {
			want = *c.Msg
		}
		if got == want || (strings.Contains(string(c.Schema), "additionalProperties") && sorted(got) == sorted(want)) {
			continue
		}
		reported++
		if reported <= 20 {
			data, _ := json.Marshal(c.Data)
			t.Errorf("schema %s data %s\n got  %q\n want %q", c.Schema, data, got, want)
		}
	}
	if reported > 0 {
		t.Errorf("%d of %d cases differ", reported, len(cases))
	}
}
