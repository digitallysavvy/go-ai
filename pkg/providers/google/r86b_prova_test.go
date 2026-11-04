package google

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseJsonLines (provider-utils response-handler.ts) skips a line only
// when line.trim() is empty and JSON.parses the line as read: a line holding
// only U+FEFF and a space is skipped, U+0085 alone and U+00A0 before the JSON
// value are JSONParseErrors (Node: JSON.parse("\u00a0{}") throws).
func TestR86bprovaGoogleResultsJSLineTrim(t *testing.T) {
	valid := `{"key":"cancelled-request","error":{"code":1,"message":"The request was cancelled."}}`
	cases := []struct {
		name  string
		body  string
		items int
		fail  bool
	}{
		{"bom-only line skipped", valid + "\n\ufeff \n", 1, false},
		{"nbsp before value", "\u00a0" + valid + "\n", 0, true},
		{"nel-only line", valid + "\n\u0085\n", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := newGoogleBatchFileResultsStream(&Batch{provider: New(Config{APIKey: "k"})}, io.NopCloser(strings.NewReader(tc.body)), "https://x.example/r", 1<<20)
			items := 0
			for {
				item, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					var jp *providererrors.JSONParseError
					if !tc.fail || !errors.As(err, &jp) {
						t.Fatalf("err = %T %v", err, err)
					}
					tc.fail = false
					break
				}
				if item == nil {
					t.Fatal("nil item")
				}
				items++
			}
			if items != tc.items || tc.fail {
				t.Fatalf("items = %d (want %d), missing JSONParseError = %v", items, tc.items, tc.fail)
			}
		})
	}
}
