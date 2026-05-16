package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAdvisor20260301_ToolShape(t *testing.T) {
	maxUses := 3
	tool := Advisor20260301(Advisor20260301Args{
		Model:   "claude-opus-4-7",
		MaxUses: &maxUses,
		Caching: &Advisor20260301Caching{Type: "ephemeral", TTL: "5m"},
	})
	if tool.Name != "anthropic.advisor_20260301" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !tool.ProviderExecuted {
		t.Fatal("advisor tool must be provider-executed")
	}
	if !tool.SupportsDeferredResults {
		t.Fatal("advisor tool must support deferred provider results")
	}
	mapper, ok := tool.ProviderOptions.(interface{ ToAnthropicAPIMap() map[string]interface{} })
	if !ok {
		t.Fatalf("provider options mapper missing: %T", tool.ProviderOptions)
	}
	m := mapper.ToAnthropicAPIMap()
	if m["type"] != "advisor_20260301" || m["name"] != "advisor" {
		t.Fatalf("advisor map mismatch: %#v", m)
	}
	if _, err := tool.Execute(t.Context(), map[string]interface{}{}, types.ToolExecutionOptions{}); err == nil {
		t.Fatal("provider-executed advisor tool should fail local execution")
	}
}
