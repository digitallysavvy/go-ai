package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS openai-responses-prepare-tools: a provider-defined tool with an unknown
// id is dropped with an `unsupported` warning.
func TestResponsesUnknownProviderToolWarns(t *testing.T) {
	tools := []types.Tool{
		{Name: "x", Type: "provider", ProviderID: "acme.search"},
		{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "web_search", Type: "provider", ProviderID: "openai.web_search"},
	}
	if got := responses.PrepareTools(tools); len(got) != 2 {
		t.Fatalf("prepared tools = %v", got)
	}
	w := responses.UnsupportedProviderToolWarnings(tools)
	if len(w) != 1 || w[0].Type != "unsupported" || w[0].Feature != "function tool [object Object]" {
		t.Fatalf("warnings = %+v", w)
	}
}
