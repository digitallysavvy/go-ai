package fileutil

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// testdata/detect_media_type_ts_golden.json holds [hexBytes, topLevelType,
// detected] triples produced by the TS SDK's detectMediaType
// (provider-utils/src/detect-media-type.ts, ai@7.0.140) over every signature,
// truncations, offsets, ID3-wrapped variants and random mutations.
func TestDetectMediaTypeSignatureMatchesTSGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/detect_media_type_ts_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][3]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	bad := 0
	for _, r := range rows {
		data, err := hex.DecodeString(r[0])
		if err != nil {
			t.Fatal(err)
		}
		got, _ := DetectMediaTypeSignature(data, r[1])
		if got != r[2] {
			bad++
			if bad <= 20 {
				t.Errorf("DetectMediaTypeSignature(%s, %q) = %q, TS = %q", r[0], r[1], got, r[2])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d mismatches of %d", bad, len(rows))
	}
}
