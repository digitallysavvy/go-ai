package jsonparser

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/fixjson_ts_golden.json holds [input, output] pairs produced by the
// TS SDK's fixJson (packages/ai/src/util/fix-json.ts, ai@7.0.140) over every
// prefix of several documents plus random token soup.
func TestFixJSONMatchesTSGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixjson_ts_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var pairs [][2]string
	if err := json.Unmarshal(raw, &pairs); err != nil {
		t.Fatal(err)
	}
	bad := 0
	for _, p := range pairs {
		if p[1] == "__THROW__" {
			continue
		}
		if got := FixJSON(p[0]); got != p[1] {
			bad++
			if bad <= 25 {
				t.Errorf("FixJSON(%q) = %q, TS = %q", p[0], got, p[1])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d mismatches of %d", bad, len(pairs))
	}
}
