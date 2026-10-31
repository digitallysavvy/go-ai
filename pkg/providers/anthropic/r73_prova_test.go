package anthropic

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/workflow"
)

func r73provaIntPtr(v int) *int { return &v }

// r73provaAllTools returns every Anthropic provider tool constructor, with
// configs where the tool takes one.
func r73provaAllTools() []types.Tool {
	no := false
	return []types.Tool{
		tools.Computer20251124(tools.Computer20251124Args{DisplayWidthPx: 1920, DisplayHeightPx: 1080, DisplayNumber: r73provaIntPtr(1), EnableZoom: true}),
		tools.Bash20250124(),
		tools.TextEditor20250728(tools.TextEditor20250728Args{MaxCharacters: r73provaIntPtr(1000)}),
		tools.CodeExecution20250825(),
		tools.CodeExecution20260120(),
		tools.Memory20250818(),
		tools.ToolSearchBm2520251119(),
		tools.ToolSearchRegex20251119(),
		tools.WebSearch20260318(tools.WebSearch20260318Config{MaxUses: r73provaIntPtr(2), BlockedDomains: []string{"a.com"}, ResponseInclusion: "excluded",
			UserLocation: &tools.WebSearchUserLocation{Type: "approximate", Country: "US", Timezone: "America/New_York"}}),
		tools.WebFetch20260318(tools.WebFetch20260318Config{MaxContentTokens: r73provaIntPtr(500), UseCache: &no, Citations: &tools.WebFetchCitations{Enabled: true}}),
		tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{Configs: map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{
			tools.ComputerToolsetZoom: {Enabled: &no, DeferLoading: &no},
		}}),
		tools.WebSearch20260209(tools.WebSearch20260209Config{AllowedDomains: []string{}}),
		tools.WebFetch20260209(tools.WebFetch20260209Config{MaxUses: r73provaIntPtr(1)}),
		tools.Advisor20260301(tools.Advisor20260301Args{Model: "claude-opus-4-8", MaxUses: r73provaIntPtr(2), Caching: &tools.Advisor20260301Caching{TTL: "1h"}}),
		tools.Computer20241022(tools.Computer20241022Args{DisplayWidthPx: 800, DisplayHeightPx: 600}),
		tools.Computer20250124(tools.Computer20241022Args{DisplayWidthPx: 800, DisplayHeightPx: 600, DisplayNumber: r73provaIntPtr(0)}),
		tools.Bash20241022(),
		tools.TextEditor20241022(),
		tools.TextEditor20250124(),
		tools.TextEditor20250429(),
		tools.CodeExecution20250522(),
		tools.WebSearch20250305(tools.WebSearch20260209Config{MaxUses: r73provaIntPtr(5)}),
		tools.WebFetch20250910(tools.WebFetch20260209Config{BlockedDomains: []string{"b.com"}}),
	}
}

func r73provaJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// CR-3: every constructor carries the TS provider tool identity (type
// "provider", id = the TS tool id) and the TS camelCase args.
func TestR73prova_ConstructorsCarryProviderIdentity(t *testing.T) {
	all := r73provaAllTools()
	if len(all) != 23 {
		t.Fatalf("got %d tools, want 23", len(all))
	}
	for _, tl := range all {
		if tl.Type != types.ToolTypeProviderDefined || tl.ProviderID != tl.Name || tl.ProviderArgs == nil {
			t.Errorf("%s: Type=%q ProviderID=%q ProviderArgs=%v", tl.Name, tl.Type, tl.ProviderID, tl.ProviderArgs)
		}
	}
	want := map[string]string{
		"anthropic.computer_20251124":         `{"displayHeightPx":1080,"displayNumber":1,"displayWidthPx":1920,"enableZoom":true}`,
		"anthropic.bash_20250124":             `{}`,
		"anthropic.text_editor_20250728":      `{"maxCharacters":1000}`,
		"anthropic.web_search_20260318":       `{"blockedDomains":["a.com"],"maxUses":2,"responseInclusion":"excluded","userLocation":{"country":"US","timezone":"America/New_York","type":"approximate"}}`,
		"anthropic.web_fetch_20260318":        `{"citations":{"enabled":true},"maxContentTokens":500,"useCache":false}`,
		"anthropic.computer_toolset_20260801": `{"configs":{"zoom":{"deferLoading":false,"enabled":false}}}`,
		"anthropic.web_search_20260209":       `{"allowedDomains":[]}`,
		"anthropic.web_fetch_20260209":        `{"maxUses":1}`,
		"anthropic.advisor_20260301":          `{"caching":{"ttl":"1h","type":"ephemeral"},"maxUses":2,"model":"claude-opus-4-8"}`,
		"anthropic.computer_20241022":         `{"displayHeightPx":600,"displayWidthPx":800}`,
		"anthropic.computer_20250124":         `{"displayHeightPx":600,"displayNumber":0,"displayWidthPx":800}`,
		"anthropic.web_search_20250305":       `{"maxUses":5}`,
		"anthropic.web_fetch_20250910":        `{"blockedDomains":["b.com"]}`,
	}
	for _, tl := range all {
		w, ok := want[tl.Name]
		if !ok {
			w = `{}`
		}
		if got := r73provaJSON(t, tl.ProviderArgs); got != w {
			t.Errorf("%s args = %s, want %s", tl.Name, got, w)
		}
	}
}

func r73provaStripIdentity(tl types.Tool) types.Tool {
	tl.Type, tl.ProviderID, tl.ProviderArgs = "", "", nil
	return tl
}

// CR-3: the direct Anthropic request is unchanged by the provider identity:
// each tool prepares to the same wire tool, betas and warnings as the same
// tool without Type/ProviderID/ProviderArgs (the v0.5 shape).
func TestR73prova_DirectAnthropicToolsUnchanged(t *testing.T) {
	for _, tl := range r73provaAllTools() {
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		base := prepareTools(prepareToolsOptions{tools: []types.Tool{r73provaStripIdentity(tl)}})
		if got.err != nil || base.err != nil {
			t.Fatalf("%s: err %v / %v", tl.Name, got.err, base.err)
		}
		if len(got.tools) != 1 || len(got.warnings) != 0 {
			t.Errorf("%s: tools=%v warnings=%v", tl.Name, got.tools, got.warnings)
		}
		if r73provaJSON(t, got.tools) != r73provaJSON(t, base.tools) || !reflect.DeepEqual(got.betas, base.betas) ||
			!reflect.DeepEqual(got.warnings, base.warnings) {
			t.Errorf("%s:\n got  %s %v %v\n base %s %v %v", tl.Name, r73provaJSON(t, got.tools), got.betas, got.warnings,
				r73provaJSON(t, base.tools), base.betas, base.warnings)
		}
	}
}

// TS prepareTools builds a provider tool from id and args. A tool rebuilt by
// workflow.ResolveSerializableTools keeps only those (its typed
// ProviderOptions become a plain map), and is sent like the original instead
// of being dropped as an unsupported provider tool.
func TestR73prova_WorkflowResolvedToolsKeepTheirWire(t *testing.T) {
	all := r73provaAllTools()
	defs, err := workflow.SerializeToolSet(all)
	if err != nil {
		t.Fatal(err)
	}
	resolved := workflow.ResolveSerializableTools(defs)
	byName := map[string]types.Tool{}
	for _, tl := range resolved {
		byName[tl.Name] = tl
	}
	for _, tl := range all {
		r, ok := byName[tl.Name]
		if !ok {
			t.Fatalf("%s missing after resolve", tl.Name)
		}
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{r}})
		want := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		if got.err != nil {
			t.Fatalf("%s: %v", tl.Name, got.err)
		}
		if r73provaJSON(t, got.tools) != r73provaJSON(t, want.tools) || !reflect.DeepEqual(got.betas, want.betas) || len(got.warnings) != 0 {
			t.Errorf("%s:\n got  %s %v %v\n want %s %v", tl.Name, r73provaJSON(t, got.tools), got.betas, got.warnings,
				r73provaJSON(t, want.tools), want.betas)
		}
	}
}

// Invalid args on a TS-shaped provider tool fail like TS validateTypes.
func TestR73prova_ProviderArgsValidated(t *testing.T) {
	bad := []types.Tool{
		{Name: "anthropic.advisor_20260301", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.advisor_20260301",
			ProviderArgs: map[string]interface{}{"model": "m", "maxTokens": 10}},
		{Name: "anthropic.web_search_20260318", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20260318",
			ProviderArgs: map[string]interface{}{"maxUses": "three"}},
	}
	for _, tl := range bad {
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		var tv *providererrors.TypeValidationError
		if got.err == nil || !errors.As(got.err, &tv) {
			t.Errorf("%s: err = %v, want a TypeValidationError", tl.Name, got.err)
		}
	}
}

// TS rejects a batch whose provider tool is sent under a different name
// (toolNameMapping.toProviderToolName(tool.name) !== tool.name). The computer
// toolset is surfaced under its toolset name "computer", which batch results
// cannot map back; with the provider identity the constructor's tool now hits
// that check.
func TestR73prova_BatchRejectsComputerToolsetConstructor(t *testing.T) {
	p := New(Config{APIKey: "k"})
	req := textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")
	req.Text.Options.Tools = []types.Tool{tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{})}
	_, err := p.ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}
