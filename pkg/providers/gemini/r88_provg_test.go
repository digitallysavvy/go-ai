package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r88provgToolChoiceNoneBody(t *testing.T, modelID string, tools []types.Tool, format *provider.ResponseFormat) string {
	t.Helper()
	m := NewLanguageModel(Config{MetadataKey: "google"}, modelID)
	opts := &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		Tools:          tools,
		ToolChoice:     types.ToolChoice{Type: types.ToolChoiceNone},
		ResponseFormat: format,
	}
	rewritten, _ := m.withJSONResponseTool(opts)
	body, _, _, err := m.buildRequest(context.Background(), rewritten, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Tools) != len(tools) {
		t.Fatal("caller's tools modified")
	}
	b, _ := json.Marshal(map[string]interface{}{"tools": body["tools"], "toolConfig": body["toolConfig"]})
	return string(b)
}

// Ports google-tool-choice-none.test.ts (TS 32dc1d2d17): toolChoice none
// omits provider-defined tools (they run server side), also before the JSON
// response tool fallback forces a call to the synthetic tool.
func TestR88provgGoogleToolChoiceNoneOmitsProviderTools(t *testing.T) {
	lookup := types.Tool{Name: "lookup", Description: "Look up", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	fileSearch := types.Tool{Type: "provider", Name: "file_search", ProviderID: "google.file_search", ProviderArgs: map[string]interface{}{"fileSearchStoreNames": []interface{}{"s"}}}
	schema := &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}}

	// Provider tools only: no tools and no toolConfig.
	if got := r88provgToolChoiceNoneBody(t, "gemini-3-flash-preview", []types.Tool{fileSearch}, nil); got != `{"toolConfig":null,"tools":null}` {
		t.Fatalf("provider-only body = %s", got)
	}
	// Mixed on Gemini 3: function declarations, mode NONE, no server-side
	// tool invocations.
	got := r88provgToolChoiceNoneBody(t, "gemini-3-flash-preview", []types.Tool{lookup, fileSearch}, nil)
	if strings.Contains(got, "fileSearch") || strings.Contains(got, "includeServerSideToolInvocations") || !strings.Contains(got, `"mode":"NONE"`) || !strings.Contains(got, `"name":"lookup"`) {
		t.Fatalf("Gemini 3 mixed body = %s", got)
	}
	// Gemini 2.5 + JSON schema: lookup + json, forced json.
	got = r88provgToolChoiceNoneBody(t, "gemini-2.5-flash", []types.Tool{lookup, fileSearch}, schema)
	if strings.Contains(got, "fileSearch") || !strings.Contains(got, `"mode":"ANY"`) || !strings.Contains(got, `"allowedFunctionNames":["json"]`) || !strings.Contains(got, `"name":"lookup"`) {
		t.Fatalf("Gemini 2.5 structured body = %s", got)
	}
	// Gemini 3 + JSON schema: lookup only, mode NONE.
	got = r88provgToolChoiceNoneBody(t, "gemini-3-flash-preview", []types.Tool{lookup, fileSearch}, schema)
	if strings.Contains(got, "fileSearch") || strings.Contains(got, `"name":"json"`) || !strings.Contains(got, `"mode":"NONE"`) {
		t.Fatalf("Gemini 3 structured body = %s", got)
	}
}
