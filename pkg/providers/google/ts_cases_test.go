package google

import (
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

const asyncHeaders = "the TS test sets async provider-level headers on the model config; the harness builds a default provider"

func TestTSCasesLanguage(t *testing.T) {
	tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = true, true
	defer func() { tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = false, false }()
	tsfixture.MetadataKeyAlias = map[string]string{"googleVertex": "google", "vertex": "google"}
	defer func() { tsfixture.MetadataKeyAlias = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemini-pro"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should pass penalty settings for Vertex Gemini 2.5 models":                                                          "Vertex-only behavior; replayed by googlevertex/ts_cases_test.go",
		"should pass imageConfig.personGeneration, imageConfig.prominentPeople and imageConfig.imageOutputOptions on Vertex": "Vertex-only behavior; replayed by googlevertex/ts_cases_test.go",
		"merges async config headers with sync request headers":                                                              asyncHeaders,
		"handles Promise-based headers":                                                                                      asyncHeaders,
		"handles async function headers from config":                                                                         asyncHeaders,
	})
}

// TestTSCasesConvertMessages replays convert-to-google-messages.test.ts.
func TestTSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		msgs, why := tsfixture.BuildPromptMessages(args[0])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := prompt.GoogleMessagesOptions{IncludeFunctionCallIDs: true, SupportsFunctionResponseParts: true} // TS defaults
		var onWarning bool
		if len(args) > 1 {
			o, _ := args[1].(map[string]interface{})
			opts.IsGemmaModel, _ = o["isGemmaModel"].(bool)
			opts.IsGemini3Model, _ = o["isGemini3Model"].(bool)
			if v, ok := o["supportsFunctionResponseParts"].(bool); ok {
				opts.SupportsFunctionResponseParts = v
			}
			if v, ok := o["includeFunctionCallIds"].(bool); ok {
				opts.IncludeFunctionCallIDs = v
			}
			if names, ok := o["providerOptionsNames"].([]interface{}); ok {
				for _, n := range names {
					opts.ProviderOptionsNames = append(opts.ProviderOptionsNames, fmt.Sprint(n))
				}
			}
			if m, ok := o["supportedFunctionResponseUrls"].(map[string]interface{}); ok {
				opts.SupportedFunctionResponseURLs = map[string][]string{}
				for k, v := range m {
					return nil, nil, fmt.Errorf("%w: supportedFunctionResponseUrls %s %v", tsfixture.ErrUnsupported, k, v)
				}
			}
			_, onWarning = o["onWarning"]
		}
		_ = onWarning
		out, err := prompt.ConvertToGoogleMessages(msgs, opts)
		if err != nil {
			return nil, nil, err
		}
		contents := out.Contents
		if contents == nil {
			contents = []map[string]interface{}{}
		}
		res := map[string]interface{}{"contents": contents}
		if out.SystemInstruction != nil {
			res["systemInstruction"] = out.SystemInstruction
		}
		return res, map[string]interface{}{"warnings": out.Warnings}, nil
	}, map[string]string{
		"throws for top-level-only image with URL source (no bytes to detect)": "Go completes a top-level media type on a URL from its extension; the image model sends reference URLs as image/*",
		"should serialize JSON Schema references in function response content": "tool result JSON arrives as a Go map, which has no key order",
	})
}

// TestTSCasesInteractions replays interactions/google-interactions-language-model.test.ts.
func TestTSCasesInteractions(t *testing.T) {
	tsfixture.DropResponseTimestamp = true
	defer func() { tsfixture.DropResponseTimestamp = false }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		p := New(Config{APIKey: "test-api-key", BaseURL: base})
		var m provider.LanguageModel
		var err error
		if agent, ok := strings.CutPrefix(id, "agent:"); ok {
			m, err = p.InteractionsAgent(agent) // provider.interactions({ agent })
		} else if managed, ok := strings.CutPrefix(id, "managedAgent:"); ok {
			m, err = p.InteractionsManagedAgent(managed) // provider.interactions({ managedAgent })
		} else {
			if id == "" {
				id = "gemini-2.5-flash"
			}
			m, err = p.Interactions(id)
		}
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"round-trips an assistant tool-call + tool-result message pair through the request body (step 2)": "tool result JSON arrives as a Go map, which has no key order",
	})
}

// TestTSCasesStructuredOutputWithTools replays structured-output-with-tools.test.ts.
func TestTSCasesStructuredOutputWithTools(t *testing.T) {
	tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = true, true
	defer func() { tsfixture.DropResponseTimestamp, tsfixture.DropResponseModelID = false, false }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/structured_output_with_tools.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemini-3-pro-preview"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}
