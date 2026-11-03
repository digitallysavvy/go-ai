package google

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestTSCasesInteractionsConvertInput replays
// interactions/convert-to-google-interactions-input.test.ts.
func TestTSCasesInteractionsConvertInput(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-2.5-flash")
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := GoogleInteractionsProviderOptions{}
		opts.PreviousInteractionID, _ = in["previousInteractionId"].(string)
		opts.MediaResolution, _ = in["mediaResolution"].(string)
		if v, ok := in["store"].(bool); ok {
			opts.Store = &v
		}
		steps, system, warnings, err := m.convertPrompt(types.Prompt{Messages: msgs}, opts)
		if err != nil {
			return nil, nil, err
		}
		res := map[string]interface{}{"input": steps, "warnings": tsfixture.WarningsToTS(warnings)}
		if system != "" {
			res["systemInstruction"] = system
		}
		return res, nil, nil
	}, nil)
}

// TestTSCasesInteractionsFinishReason replays
// interactions/map-google-interactions-finish-reason.test.ts.
func TestTSCasesInteractionsFinishReason(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_finish_reason.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		status, _ := in["status"].(string)
		hasFn, _ := in["hasFunctionCall"].(bool)
		return string(mapInteractionsFinishReason(status, hasFn)), nil, nil
	}, nil)
}

// TestTSCasesInteractionsPrepareTools replays
// interactions/prepare-google-interactions-tools.test.ts.
func TestTSCasesInteractionsPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice = tsfixture.BuildToolChoice(tc)
		}
		tools, toolChoice, warnings := prepareInteractionsTools(ts, choice)
		return map[string]interface{}{
			"tools":        tools,
			"toolChoice":   toolChoice,
			"toolWarnings": tsfixture.WarningsToTS(warnings),
		}, nil, nil
	}, nil)
}

// r83provaGenerateID mirrors the TS test generators: parse-google-interactions-
// outputs.test.ts returns 'gen-id'; extract-google-interactions-sources.test.ts
// counts `id-${++nextId}` from 0 per test.
func r83provaGenerateID(v interface{}) func() string {
	m, _ := v.(map[string]interface{})
	src, _ := m["src"].(string)
	if strings.Contains(src, "'gen-id'") {
		return func() string { return "gen-id" }
	}
	n := 0
	return func() string { n++; return fmt.Sprintf("id-%d", n) }
}

func r83provaDecode(v interface{}, out interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func r83provaContentToTS(parts []types.ContentPart) []interface{} {
	got, _ := tsfixture.GenerateContentToTS(&types.GenerateResult{Content: parts}).([]interface{})
	if got == nil {
		got = []interface{}{}
	}
	return got
}

// TestTSCasesInteractionsParseOutputs replays
// interactions/parse-google-interactions-outputs.test.ts.
func TestTSCasesInteractionsParseOutputs(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_parse_outputs.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		var steps []interactionsContentBlock
		if err := r83provaDecode(in["steps"], &steps); err != nil {
			return nil, nil, err
		}
		m := NewInteractionsLanguageModelWithConfig(InteractionsConfig{GenerateID: r83provaGenerateID(in["generateId"])}, "gemini-2.5-flash")
		interactionID, _ := in["interactionId"].(string)
		content, _, hasFn := m.parseOutputs(steps, interactionID)
		return map[string]interface{}{"content": r83provaContentToTS(content), "hasFunctionCall": hasFn}, nil, nil
	}, nil)
}

// TestTSCasesInteractionsParseOutputsEmptyBuiltinID ports "generates an ID
// when a built-in tool call ID is empty" (the steps are an inline cast the
// extractor does not evaluate).
func TestTSCasesInteractionsParseOutputsEmptyBuiltinID(t *testing.T) {
	m := NewInteractionsLanguageModelWithConfig(InteractionsConfig{GenerateID: func() string { return "gen-id" }}, "gemini-2.5-flash")
	var steps []interactionsContentBlock
	if err := json.Unmarshal([]byte(`[{"type":"google_search_call","id":"","arguments":{"query":"weather"}}]`), &steps); err != nil {
		t.Fatal(err)
	}
	content, _, _ := m.parseOutputs(steps, "")
	got := r83provaContentToTS(content)
	want := map[string]interface{}{"type": "tool-call", "toolCallId": "gen-id", "toolName": "google_search", "providerExecuted": true}
	if len(got) == 0 {
		t.Fatal("no content")
	}
	part, _ := got[0].(map[string]interface{})
	for k, v := range want {
		if part[k] != v {
			t.Errorf("content[0].%s = %v, TS %v", k, part[k], v)
		}
	}
}

// TestTSCasesInteractionsSources replays
// interactions/extract-google-interactions-sources.test.ts (annotationToSource,
// annotationsToSources, builtinToolResultToSources).
func TestTSCasesInteractionsSources(t *testing.T) {
	all := tsfixture.LoadCases(t, "testdata/tscases/interactions_sources.json.gz")
	byFn := map[string][]tsfixture.Case{}
	for _, c := range all {
		byFn[c.TopDescribe] = append(byFn[c.TopDescribe], c)
	}
	if len(byFn) != 3 {
		t.Fatalf("describes = %d, want 3", len(byFn))
	}
	sourcesFns := map[string]func(in map[string]interface{}) ([]interface{}, error){
		"annotationsToSources": func(in map[string]interface{}) ([]interface{}, error) {
			var anns []interactionsAnnotation
			err := r83provaDecode(in["annotations"], &anns)
			return r83provaContentToTS(annotationsToSources(anns, r83provaGenerateID(in["generateId"]))), err
		},
		"builtinToolResultToSources": func(in map[string]interface{}) ([]interface{}, error) {
			var block interactionsContentBlock
			err := r83provaDecode(in["block"], &block)
			return r83provaContentToTS(builtinToolResultSources(block, r83provaGenerateID(in["generateId"]))), err
		},
	}
	// The harness does not evaluate the ternary map these tests assert
	// (`sources.map(s => (s.sourceType === 'url' ? s.url : null))`); check it
	// here.
	mapped := 0
	for _, c := range all {
		for _, a := range c.Asserts {
			if a.Expr != "sources.map(s => (s.sourceType === 'url' ? s.url : null))" {
				continue
			}
			mapped++
			in, _ := c.FnArgs[0].(map[string]interface{})
			got, err := sourcesFns[c.TopDescribe](in)
			if err != nil {
				t.Fatalf("%s: %v", c.Test, err)
			}
			urls := make([]interface{}, 0, len(got))
			for _, g := range got {
				s, _ := g.(map[string]interface{})
				if s["sourceType"] == "url" {
					urls = append(urls, s["url"])
				} else {
					urls = append(urls, nil)
				}
			}
			if d := tsfixture.DiffValues(a.Arg, urls); d != "" {
				t.Errorf("%s: %s", c.Test, d)
			}
		}
	}
	if mapped != 2 {
		t.Errorf("ternary map assertions = %d, want 2", mapped)
	}
	t.Run("annotationToSource", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["annotationToSource"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var ann interactionsAnnotation
			if err := r83provaDecode(in["annotation"], &ann); err != nil {
				return nil, nil, err
			}
			got := r83provaContentToTS(annotationsToSources([]interactionsAnnotation{ann}, r83provaGenerateID(in["generateId"])))
			if len(got) == 0 {
				return nil, nil, nil
			}
			return got[0], nil, nil
		}, nil)
	})
	t.Run("annotationsToSources", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["annotationsToSources"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var anns []interactionsAnnotation
			if u, _ := in["annotations"].(map[string]interface{}); u["__undef"] == true {
				in["annotations"] = nil
			}
			if err := r83provaDecode(in["annotations"], &anns); err != nil {
				return nil, nil, err
			}
			return r83provaContentToTS(annotationsToSources(anns, r83provaGenerateID(in["generateId"]))), nil, nil
		}, nil)
	})
	t.Run("builtinToolResultToSources", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["builtinToolResultToSources"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var block interactionsContentBlock
			if err := r83provaDecode(in["block"], &block); err != nil {
				return nil, nil, err
			}
			return r83provaContentToTS(builtinToolResultSources(block, r83provaGenerateID(in["generateId"]))), nil, nil
		}, nil)
	})
}
