package huggingface

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPrepareResponsesToolsProviderToolWarningNames(t *testing.T) {
	_, _, _, warnings := prepareResponsesTools([]types.Tool{
		{Type: types.ToolTypeProviderDefined, ProviderID: "x.search", Name: "search"},
		{Type: types.ToolTypeProviderDefined, Name: "bare"},
	}, types.ToolChoice{})
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if warnings[0].Feature != "provider-defined tool x.search" {
		t.Fatalf("feature 0 = %q", warnings[0].Feature)
	}
	if warnings[1].Feature != "provider-defined tool bare" {
		t.Fatalf("feature 1 = %q", warnings[1].Feature)
	}
}
