package openresponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR97provbStreamMissingMemberTypeError: TS validates Open Responses
// chunks with z.any() and then reads chunk.item.type, chunk.response.* or
// chunk.error.code; a missing or null member is a TypeError that errors the
// stream (no finish part). Go skipped the frame. A non-object member is
// read without a throw.
func TestR97provbStreamMissingMemberTypeError(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"response.output_item.added"}`:            "Cannot read properties of undefined (reading 'type')",
		`{"type":"response.output_item.done","item":null}`: "Cannot read properties of null (reading 'type')",
		`{"type":"response.completed"}`:                    "Cannot read properties of undefined (reading 'incomplete_details')",
		`{"type":"response.failed","response":null}`:       "Cannot read properties of null (reading 'error')",
		`{"type":"error"}`:                                 "Cannot read properties of undefined (reading 'code')",
		`{"type":"response.output_item.added","item":"x"}`: "",
	} {
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader("data: "+frame+"\n\n")), nil)
		var err error
		sawFinish := false
		for err == nil {
			var c *provider.StreamChunk
			c, err = s.Next()
			if c != nil && c.Type == provider.ChunkTypeFinish {
				sawFinish = true
			}
		}
		if want == "" {
			if err != io.EOF || !sawFinish {
				t.Errorf("%s: err = %v, finish %v; want a clean end", frame, err, sawFinish)
			}
			continue
		}
		if sawFinish || err.Error() != want {
			t.Errorf("%s: err = %v, finish %v; want %q and no finish", frame, err, sawFinish, want)
		}
	}
}

// TestR97provbExtensionStreamPartsKeepFields: TS enqueues each part an
// extension's decodeItem returns as is, so a streamed tool-result keeps its
// providerMetadata (the {id, itemId} extension reference), isError and
// providerExecuted, and a tool-call keeps dynamic (Go dropped them).
func TestR97provbExtensionStreamPartsKeepFields(t *testing.T) {
	isErr := true
	ext := Extension{
		ID:        "lmstudio.r97",
		ItemTypes: []string{"lmstudio:r97"},
		DecodeItem: func(item ExtensionItem, mode string) ([]types.ContentPart, error) {
			return []types.ContentPart{
				types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: "{}", Dynamic: true},
				types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Result: "r", IsError: &isErr, ProviderExecuted: true},
			}, nil
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{ext}})
	var item OutputItem
	if err := json.Unmarshal([]byte(`{"type":"lmstudio:r97","id":"x_1","status":"completed"}`), &item); err != nil {
		t.Fatal(err)
	}
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader("")), nil, "open-responses")
	s.extensionRegistry = p.extensionRegistry
	s.handleStreamEvent(&StreamEvent{Type: "response.output_item.done", Item: &item})
	var call *types.ToolCall
	var result *types.ToolResult
	for _, c := range s.pending {
		if c.ToolCall != nil {
			call = c.ToolCall
		}
		if c.ToolResult != nil {
			result = c.ToolResult
		}
	}
	if call == nil || !call.Dynamic {
		t.Fatalf("tool-call = %#v, want dynamic", call)
	}
	ref, _ := result.ProviderMetadata["open-responses"].(map[string]interface{})
	extRef, _ := ref["openResponsesExtension"].(map[string]interface{})
	if result == nil || extRef["itemId"] != "x_1" || result.IsError == nil || !*result.IsError || !result.ProviderExecuted {
		t.Fatalf("tool-result = %#v, want the extension reference, isError and providerExecuted", result)
	}
}

// TestR97provbUsageAbsentTotals: TS reads total: usage.input_tokens /
// output_tokens as sent, so an absent count is undefined (noCache and text
// still compute with ?? 0); Go reported 0. Core's totalTokens adds the
// totals that are present.
func TestR97provbUsageAbsentTotals(t *testing.T) {
	var u Usage
	if err := json.Unmarshal([]byte(`{"output_tokens":5}`), &u); err != nil {
		t.Fatal(err)
	}
	got := convertOpenResponsesUsage(&u)
	if got.InputTokens != nil || got.OutputTokens == nil || *got.OutputTokens != 5 ||
		got.TotalTokens == nil || *got.TotalTokens != 5 ||
		got.InputDetails == nil || got.InputDetails.NoCacheTokens == nil || *got.InputDetails.NoCacheTokens != 0 {
		t.Fatalf("usage = %+v, want input total undefined, output 5, total 5, noCache 0", got)
	}
	if err := json.Unmarshal([]byte(`{}`), &u); err != nil {
		t.Fatal(err)
	}
	if got := convertOpenResponsesUsage(&u); got.InputTokens != nil || got.OutputTokens != nil || got.TotalTokens != nil {
		t.Fatalf("usage = %+v, want every total undefined", got)
	}
}
