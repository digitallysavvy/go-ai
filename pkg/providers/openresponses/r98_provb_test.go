package openresponses

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR98provbAsIntPlatformInt: a replayed annotation index read as an
// int64 or json.Number is clamped to the platform int like the float64 case
// (a plain conversion wraps on 32-bit targets).
func TestR98provbAsIntPlatformInt(t *testing.T) {
	want := int64(math.MaxInt)
	if want > 3_000_000_000 {
		want = 3_000_000_000
	}
	for _, in := range []interface{}{int64(3_000_000_000), json.Number("3000000000"), float64(3_000_000_000)} {
		got, ok := asInt(in)
		if !ok || int64(got) != want {
			t.Errorf("%T: %d, want %d", in, got, want)
		}
	}
	if got, _ := asInt(int64(-3_000_000_000)); got > 0 {
		t.Errorf("negative int64: %d, want a negative value", got)
	}
}

// TestR98provbExtensionGenerateToolCallKeepsFields: doGenerate pushes each
// part an extension's decodeItem returns as is, so a dynamic tool-call with
// a title keeps both. Core rebuilds the content part from the result's tool
// call, which dropped them (the stream path was fixed in r97).
func TestR98provbExtensionGenerateToolCallKeepsFields(t *testing.T) {
	ext := Extension{
		ID:        "lmstudio.r98",
		ItemTypes: []string{"lmstudio:r98"},
		DecodeItem: func(item ExtensionItem, mode string) ([]types.ContentPart, error) {
			return []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Title: "T", Input: "{}", Dynamic: true}}, nil
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{ext}})
	var item OutputItem
	if err := json.Unmarshal([]byte(`{"type":"lmstudio:r98","id":"x_1","status":"completed"}`), &item); err != nil {
		t.Fatal(err)
	}
	res, err := NewLanguageModel(p, "m").convertResponse(OpenResponsesResponse{Output: []OutputItem{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].Dynamic || res.ToolCalls[0].Title != "T" {
		t.Fatalf("tool calls = %+v, want dynamic with title T", res.ToolCalls)
	}
}
