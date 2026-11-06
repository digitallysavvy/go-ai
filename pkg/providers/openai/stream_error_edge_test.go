package openai

import (
	"encoding/json"
	"testing"
)

// TS accepts any string message (including "") and any string type (including "").
func TestParseOpenAIStreamError_EmptyStringsAccepted(t *testing.T) {
	if f := parseOpenAIStreamError(json.RawMessage(`{"message":"","type":""}`)); f == nil {
		t.Fatal("empty type string should still count as an error frame")
	}
	f := parseOpenAIStreamError(json.RawMessage(`{"error":{"message":""}}`))
	if f == nil || f.Message != "" {
		t.Fatalf("empty message in nested error should parse: %+v", f)
	}
	if parseOpenAIStreamError(json.RawMessage(`{"message":"x"}`)) != nil {
		t.Fatal("bare message without code/type/param is not an error frame")
	}
}
