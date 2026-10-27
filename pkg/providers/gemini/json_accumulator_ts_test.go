package gemini

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// tsAccumulatorCase is one accumulator run recorded from the TS
// GoogleJSONAccumulator (TS e64ed82f27): the chunks fed to processPartialArgs,
// the {currentJSON, textDelta} each returned, and finalize().
//
//   - "ts-test-N": every sequence the TS accumulator tests run (empty
//     containers, out-of-order and repeated paths, prototype-pollution keys,
//     string continuation, ...), one case per accumulator instance.
//   - "gen-consistent-N": random sequences where each path keeps one value kind
//     (sparse arrays, repeated paths, empties, escapes, number formats).
//   - "gen-ts-throws-N": random sequences where TS throws a TypeError. Go has
//     to fail at the same chunk instead of panicking.
//
// Sequences where TS coerces conflicting values instead of throwing
// (null + "x" == "nullx", {} + "x" == "[object Object]x") are not replayed: Go
// fails on them too, with an error rather than the coerced text. See
// testdata/ts_accumulator_gen for the generators.
type tsAccumulatorCase struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Chunks [][]PartialArg
	Steps  []struct {
		Current map[string]interface{} `json:"current"`
		Delta   string                 `json:"delta"`
		Err     string                 `json:"err"`
	} `json:"steps"`
	Fin *struct {
		FinalJSON string `json:"finalJSON"`
		Closing   string `json:"closing"`
	} `json:"fin"`
}

func loadTSAccumulatorCases(t *testing.T) []tsAccumulatorCase {
	t.Helper()
	f, err := os.Open("testdata/ts_accumulator_cases.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var cases []tsAccumulatorCase
	for sc.Scan() {
		var raw struct {
			tsAccumulatorCase
			Chunks [][]PartialArg `json:"chunks"`
		}
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		c := raw.tsAccumulatorCase
		c.Chunks = raw.Chunks
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestGoogleJSONAccumulator_MatchesTSRecordings(t *testing.T) {
	cases := loadTSAccumulatorCases(t)
	if len(cases) < 400 {
		t.Fatalf("only %d recorded cases", len(cases))
	}
	kinds := map[string]int{}
	for _, c := range cases {
		kinds[strings.SplitN(c.Name, "-", 3)[0]+"-"+strings.SplitN(c.Name, "-", 3)[1]]++
		c := c
		t.Run(c.Name, func(t *testing.T) {
			acc := NewGoogleJSONAccumulator()
			for i, chunk := range c.Chunks {
				if i >= len(c.Steps) {
					break
				}
				got, err := acc.processPartialArgs(chunk)
				want := c.Steps[i]
				if want.Err != "" {
					if err == nil {
						t.Fatalf("chunk %d: TS throws (%s), Go returned %q", i, want.Err, got.TextDelta)
					}
					return
				}
				if err != nil {
					if c.Kind == "error" {
						return // TS coerced an earlier conflict that Go rejects
					}
					t.Fatalf("chunk %d: unexpected error: %v", i, err)
				}
				if got.TextDelta != want.Delta {
					t.Fatalf("chunk %d: textDelta = %q, TS %q", i, got.TextDelta, want.Delta)
				}
				var plain map[string]interface{}
				b, _ := json.Marshal(got.CurrentJSON)
				_ = json.Unmarshal(b, &plain)
				if !reflect.DeepEqual(plain, want.Current) {
					t.Fatalf("chunk %d: currentJSON = %s, TS %v", i, b, want.Current)
				}
			}
			if c.Kind == "error" {
				t.Fatal("TS throws but Go did not")
			}
			if c.Fin != nil {
				finalJSON, closing := acc.Finalize()
				if finalJSON != c.Fin.FinalJSON || closing != c.Fin.Closing {
					t.Fatalf("finalize = (%q, %q), TS (%q, %q)", finalJSON, closing, c.Fin.FinalJSON, c.Fin.Closing)
				}
			}
		})
	}
	if kinds["ts-test"] < 37 {
		t.Fatalf("expected the 37 TS accumulator tests, got %v", kinds)
	}
}

// A path that conflicts with an earlier value must fail, not panic (TS throws
// a TypeError or builds a value JSON.stringify drops).
func TestGoogleJSONAccumulator_ConflictingPathsFailWithoutPanic(t *testing.T) {
	str := func(path, v string) PartialArg { return PartialArg{JSONPath: path, StringValue: &v} }
	num := func(path string, v float64) PartialArg { return PartialArg{JSONPath: path, NumberValue: &v} }
	tests := []struct {
		name string
		args []PartialArg
	}{
		{"key under a string", []PartialArg{str("$.a", "x"), str("$.a.b", "y")}},
		{"key under a number", []PartialArg{num("$.a", 1), num("$.a.b", 2)}},
		{"index on an object", []PartialArg{str("$.a.b", "x"), str("$.a[0]", "y")}},
		{"key on an array", []PartialArg{str("$.a[0]", "x"), str("$.a.b", "y")}},
		{"root index", []PartialArg{str("$.[0]", "x")}},
		{"string onto number", []PartialArg{num("$.a", 1), str("$.a", "x")}},
		{"index beyond the supported maximum", []PartialArg{str("$.a[999999999]", "x")}},
		{"index beyond int", []PartialArg{str("$.a[99999999999999999999999]", "x")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acc := NewGoogleJSONAccumulator()
			if _, err := acc.processPartialArgs(tc.args); err == nil {
				t.Fatal("expected an error")
			}
			acc.ProcessPartialArgs(tc.args) // must not panic either
		})
	}
}

// Skipped array slots are holes: a string for one starts a value (no
// continuation), and they print as null. The stream text and the final JSON
// still agree because the closing delta is counted in UTF-16 code units.
func TestGoogleJSONAccumulator_SparseArrayAndUTF16Closing(t *testing.T) {
	s := func(path, v string) PartialArg { return PartialArg{JSONPath: path, StringValue: &v} }
	acc := NewGoogleJSONAccumulator()
	r1 := acc.ProcessPartialArgs([]PartialArg{s("$.l[2]", "c"), s("$.l[0]", "😀")})
	if got, want := r1.TextDelta, `{"l":["c","😀"`; got != want {
		t.Fatalf("textDelta = %q, want %q", got, want)
	}
	final, closing := acc.Finalize()
	if want := `{"l":["😀",null,"c"]}`; final != want {
		t.Fatalf("final = %q, want %q", final, want)
	}
	// TS slices the final JSON by the UTF-16 length of the streamed text.
	if closing == "" || strings.Contains(closing, "�") {
		t.Fatalf("closing delta = %q", closing)
	}
}

// A conflicting path in a streamed function call ends the stream with an error
// after the chunks already produced; it must not panic the reader.
func TestStream_PartialArgsConflictEndsStreamWithError(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"f","partialArgs":[{"jsonPath":"$.a","stringValue":"x","willContinue":true}]}}]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.a.b","numberValue":1}]}}]}}]}`
	s := newTestStream(sseStream(chunk1, chunk2, "[DONE]"))
	var types []provider.ChunkType
	for {
		c, err := s.Next()
		if err != nil {
			if err.Error() == "EOF" {
				t.Fatal("stream ended normally; want an error")
			}
			if !strings.Contains(err.Error(), "partialArgs") {
				t.Fatalf("error = %v", err)
			}
			break
		}
		types = append(types, c.Type)
	}
	if len(types) == 0 {
		t.Fatal("chunks produced before the failure were dropped")
	}
}
