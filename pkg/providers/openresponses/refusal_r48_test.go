package openresponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate pushes a text part for every message content part. A refusal
// part has no `text`, so it becomes an empty text part that keeps the item id
// (and does not change the joined text).
func TestR48_GenerateRefusalContentPartBecomesEmptyTextPart(t *testing.T) {
	var response OpenResponsesResponse
	body := `{"id":"r","output":[{"type":"message","id":"msg_1","content":[` +
		`{"type":"output_text","text":"hi"},{"type":"refusal","refusal":"no"}]}]}`
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	result, err := (&LanguageModel{}).convertResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v", result.Content)
	}
	refusal := result.Content[1].(types.TextContent)
	if refusal.Text != "" || len(refusal.ProviderMetadata) == 0 {
		t.Fatalf("refusal part = %#v", refusal)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(refusal.ProviderMetadata, &meta); err != nil || meta["open-responses"]["itemId"] != "msg_1" {
		t.Fatalf("metadata = %s (%v)", refusal.ProviderMetadata, err)
	}
	if result.Text != "hi" {
		t.Fatalf("text = %q", result.Text)
	}
}

// An empty-string reasoningEffort option is a resolved value in TS (`??` only
// skips null/undefined): it is sent and the top-level reasoning is not mapped.
func TestR48_EmptyReasoningEffortOptionIsSent(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:1234/v1"}), "gpt-5")
	high := types.ReasoningHigh
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       &high,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": ""}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, _ := body["reasoning"].(map[string]interface{})
	if v, ok := reasoning["effort"]; !ok || v != "" {
		t.Fatalf("reasoning = %v", body["reasoning"])
	}
}

// TS deletes the function-call accumulator when the item is done, so a second
// done event for the same item with empty arguments carries no input.
func TestR48_StreamFunctionCallStateDeletedOnDone(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","item":{"type":"function_call","id":"i","call_id":"c","name":"f","arguments":"{\"a\":1}"}}` + "\n\n" +
		`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"i","call_id":"c","name":"f","arguments":""}}` + "\n\n" +
		`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"i","call_id":"c","name":"f","arguments":""}}` + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse)), nil)
	var raws []string
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolCall {
			raws = append(raws, c.ToolCall.RawArguments)
		}
	}
	if len(raws) != 2 || raws[0] != `{"a":1}` || raws[1] != "" {
		t.Fatalf("raw arguments = %q", raws)
	}
}
