package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// prova CR (carry CR §2, round 96): the Converse usage object and
// AmazonBedrockCacheDetailSchema are .catchall(z.json()), and their declared
// z.number() fields reject a non-finite number: JSON.parse of 1e400 there
// fails like zod, a finite value passes. Both the response schema and the
// stream metadata check.
func TestR96integUsageCatchallAndFiniteNumbers(t *testing.T) {
	response := func(usage string) []byte {
		return []byte(`{"output":{"message":{"content":[{"text":"a"}],"role":"assistant"}},"stopReason":"end_turn","usage":` + usage + `}`)
	}
	metadata := func(usage string) interface{} {
		var v interface{}
		if err := jsonexact.Unmarshal([]byte(`{"usage":`+usage+`}`), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	ok := []string{
		`{"inputTokens":1,"outputTokens":1,"totalTokens":2,"x":[1,"a"]}`,
		`{"inputTokens":1,"outputTokens":1,"totalTokens":2,"cacheDetails":[{"inputTokens":1,"ttl":"5m","y":{"z":1}}]}`,
	}
	bad := []string{
		`{"inputTokens":1,"outputTokens":1,"totalTokens":2,"x":[1e400]}`,
		`{"inputTokens":1,"outputTokens":1,"totalTokens":2,"cacheDetails":[{"inputTokens":1,"ttl":"5m","y":-1e400}]}`,
		`{"inputTokens":1e400,"outputTokens":1,"totalTokens":2}`,
	}
	validate := bedrockValidator(zbConverse)
	for _, u := range ok {
		if err := validate(200, nil, response(u)); err != nil {
			t.Errorf("response %s: err = %v", u, err)
		}
		if err := validateConverseStreamEvent("metadata", metadata(u)); err != nil {
			t.Errorf("metadata %s: err = %v", u, err)
		}
	}
	for _, u := range bad {
		if err := validate(200, nil, response(u)); err == nil {
			t.Errorf("response %s: err = nil, want Invalid JSON response", u)
		}
		if err := validateConverseStreamEvent("metadata", metadata(u)); err == nil {
			t.Errorf("metadata %s: err = nil, want a validation error", u)
		}
	}
}
