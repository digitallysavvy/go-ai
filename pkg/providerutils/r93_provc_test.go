package providerutils

import (
	"net/http"
	"testing"
)

// TestR93provcIntNonnegIsSafeInteger: zod 4 z.number().int() accepts only
// safe integers, so z.number().int().nonnegative() rejects 2^53 and 1e300
// (node-verified with zod 4.6.7). Go accepted any whole number.
func TestR93provcIntNonnegIsSafeInteger(t *testing.T) {
	cases := map[string]bool{
		`{"id":"f","bytes":9007199254740991}`: true,
		`{"id":"f","bytes":9007199254740992}`: false,
		`{"id":"f","bytes":1e300}`:            false,
		`{"id":"f","bytes":-1}`:               false,
		`{"id":"f","bytes":1.5}`:              false,
	}
	validate := MediaResponseValidator("deepseek", SchemaDeepSeekFile)
	for body, ok := range cases {
		if err := validate(http.StatusOK, nil, []byte(body)); (err == nil) != ok {
			t.Errorf("%s: err = %v, want ok=%v", body, err, ok)
		}
	}
}
