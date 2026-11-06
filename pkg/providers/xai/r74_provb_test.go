package xai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR74ProvbToolArgsTypeValidationText: TS prepareResponsesTools validates
// provider tool args with validateTypes (no context), so the error is
// TypeValidationError "Type validation failed: Value: <args>.\nError
// message: <zod issues>". The expected issue lists are zod 4 run on the TS
// args schemas. v0.5/r73 Go said "validation error on field 'tools': Type
// validation failed for <tool>: ..." and did not type-check web search / X
// search args.
func TestR74ProvbToolArgsTypeValidationText(t *testing.T) {
	many := func(n int) []string { return make([]string, n) }
	tsShaped := func(id string, args map[string]interface{}) types.Tool {
		return types.Tool{Name: id, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}
	}
	tooBig := func(key string, max string) string {
		return "  {\n    \"origin\": \"array\",\n    \"code\": \"too_big\",\n    \"maximum\": " + max + ",\n    \"inclusive\": true,\n    \"path\": [\n      \"" + key + "\"\n    ],\n    \"message\": \"Too big: expected array to have <=" + max + " items\"\n  }"
	}
	invalidType := func(expected, received string, path ...string) string {
		return "  {\n    \"expected\": \"" + expected + "\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      " + strings.Join(path, ",\n      ") + "\n    ],\n    \"message\": \"Invalid input: expected " + expected + ", received " + received + "\"\n  }"
	}
	for _, tc := range []struct {
		name  string
		tool  types.Tool
		value string
		want  string
	}{
		{"6 domains", WebSearch(WebSearchConfig{AllowedDomains: many(6)}), `{"allowedDomains":["","","","","",""]}`, r74provbIssues(tooBig("allowedDomains", "5"))},
		{"21 handles", XSearch(XSearchConfig{ExcludedXHandles: many(21)}), "", r74provbIssues(tooBig("excludedXHandles", "20"))},
		{"web search types", tsShaped("xai.web_search", map[string]interface{}{"enableImageSearch": "x", "excludedDomains": []interface{}{1.0}}), `{"enableImageSearch":"x","excludedDomains":[1]}`,
			r74provbIssues(invalidType("string", "number", `"excludedDomains"`, "0"), invalidType("boolean", "string", `"enableImageSearch"`))},
		{"mcp no url", tsShaped("xai.mcp", map[string]interface{}{}), `{}`, r74provbIssues(invalidType("string", "undefined", `"serverUrl"`))},
		{"mcp header", tsShaped("xai.mcp", map[string]interface{}{"serverUrl": "u", "headers": map[string]interface{}{"a": 1.0}}), "", r74provbIssues(invalidType("string", "number", `"headers"`, `"a"`))},
		{"file search no ids", FileSearch(FileSearchConfig{}), `{}`, r74provbIssues(invalidType("array", "undefined", `"vectorStoreIds"`))},
		{"image action", ImageGeneration(ImageGenerationConfig{Action: "remix"}), `{"action":"remix"}`,
			r74provbIssues("  {\n    \"code\": \"invalid_value\",\n    \"values\": [\n      \"auto\",\n      \"generate\",\n      \"edit\"\n    ],\n    \"path\": [\n      \"action\"\n    ],\n    \"message\": \"Invalid option: expected one of \\\"auto\\\"|\\\"generate\\\"|\\\"edit\\\"\"\n  }")},
	} {
		err := validateXAIResponsesToolArgs([]types.Tool{tc.tool})
		var tv *providererrors.TypeValidationError
		if !errors.As(err, &tv) {
			t.Errorf("%s: err = %v, want a TypeValidationError", tc.name, err)
			continue
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "Type validation failed: Value: ") || !strings.HasSuffix(msg, ".\nError message: "+tc.want) {
			t.Errorf("%s: err =\n%s\nwant issues\n%s", tc.name, msg, tc.want)
		}
		if tc.value != "" && !strings.HasPrefix(msg, "Type validation failed: Value: "+tc.value+".\n") {
			t.Errorf("%s: value in %q, want %s", tc.name, msg, tc.value)
		}
	}
	// TS fileSearch({vectorStoreIds: undefined}) has no vectorStoreIds arg.
	if _, ok := FileSearch(FileSearchConfig{}).ProviderArgs["vectorStoreIds"]; ok {
		t.Error("FileSearch without IDs sets a vectorStoreIds arg")
	}
}

// r74provbIssues renders zod issue objects as ZodError.message lists them.
func r74provbIssues(lines ...string) string { return "[\n" + strings.Join(lines, ",\n") + "\n]" }

// TestR74ProvbTSCasesPrepareTools replays xai-responses-prepare-tools.test.ts
// (TS prepareResponsesTools) through the whole xAI Responses request builder,
// with the provider tools in TS shape ({type: "provider", id, name, args}).
func TestR74ProvbTSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		if args == nil {
			args = map[string]interface{}{}
		}
		return types.Tool{Name: name, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		tools, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: tools}
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok && tc["__undef"] == nil {
			opts.ToolChoice = tsfixture.BuildToolChoice(tc)
		}
		body, warnings, err := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4").buildRequestBody(opts, false)
		if err != nil {
			return nil, nil, err
		}
		out := map[string]interface{}{"toolWarnings": tsfixture.WarningsToTS(warnings)}
		for key, field := range map[string]string{"tools": "tools", "tool_choice": "toolChoice"} {
			if v, ok := body[key]; ok {
				b, _ := json.Marshal(v)
				var g interface{}
				_ = json.Unmarshal(b, &g)
				out[field] = g
			}
		}
		return out, nil, nil
	}, nil)
}
