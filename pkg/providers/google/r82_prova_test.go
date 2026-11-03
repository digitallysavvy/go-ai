package google

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provaPNGBadTail is a PNG signature in standard base64 followed by a tail
// that is not base64: TS detectMediaType decodes only the sniffed prefix
// (decodePrefix: ceil(18/3)*4 characters), so the type is still detected and
// the string is forwarded as is (convertToBase64).
const r82provaPNGBadTail = "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAA!!not-base64!!"

// r82provaBadPrefix has a prefix atob rejects: TS detectMediaType throws.
const r82provaBadPrefix = "!!!!iVBORw0KGgoAAAAAAAAAAAAAAAA"

func r82provaDataFile(mediaType, data string) types.FileContent {
	return types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, MediaType: mediaType, DataString: data}}
}

func r82provaIsDecodeError(err error) bool {
	var unsupported *providererrors.UnsupportedFunctionalityError
	return err != nil && !errors.As(err, &unsupported)
}

func TestR82provaGoogleInteractionsSniffDecodesOnlyPrefix(t *testing.T) {
	block, _, err := fileContentToInteractionBlock(r82provaDataFile("image", r82provaPNGBadTail), "")
	if err != nil || block["mime_type"] != "image/png" || block["data"] != r82provaPNGBadTail {
		t.Fatalf("block = %v, %v", block, err)
	}
	if _, _, err := fileContentToInteractionBlock(r82provaDataFile("image", r82provaBadPrefix), ""); !r82provaIsDecodeError(err) {
		t.Fatalf("bad prefix: err = %v", err)
	}
}

// TS copies a response signature when `signature != null`, so the empty
// signature the API sends on function_call steps (recorded TS test
// "emits tool-input-start, ... for a function_call stream (step 1)") is kept
// in providerMetadata, and sent back on the next turn; Go dropped "".
func TestR82provaInteractionsEmptySignatureKept(t *testing.T) {
	parts, _ := r80provaRunInteractions(t,
		`{"event_type":"interaction.created","interaction":{"id":"interaction-1","status":"in_progress"}}`,
		`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"c1","name":"f","signature":"","arguments":{}}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.completed","interaction":{"id":"interaction-1","status":"requires_action"}}`,
	)
	calls := r80provaFilter(parts, "tool-call")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].(map[string]interface{})["providerMetadata"], map[string]interface{}{"google": map[string]interface{}{"signature": "", "interactionId": "interaction-1"}}) {
		t.Fatalf("tool-call parts = %v", calls)
	}

	m := &InteractionsLanguageModel{}
	content, _, _ := m.parseOutputs([]interactionsContentBlock{{Type: "function_call", ID: "c1", Name: "f", Signature: signaturePtr("x")}, {Type: "thought", Signature: new(string)}}, "interaction-1")
	reasoning, ok := content[1].(types.ReasoningContent)
	if !ok || string(reasoning.ProviderMetadata) != `{"google":{"interactionId":"interaction-1","signature":""}}` {
		t.Fatalf("thought content = %#v", content[1])
	}

	steps, _, err := m.convertAssistantSteps([]types.ContentPart{
		types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: "{}", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"signature": ""}}},
		types.ReasoningContent{Text: "t", ProviderMetadata: json.RawMessage(`{"google":{"signature":""}}`)},
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range steps {
		if sig, ok := step["signature"]; !ok || sig != "" {
			t.Fatalf("step %d = %v, want signature \"\"", i, step)
		}
	}
}

// TS "should pass headers" builds the provider with provider-level headers
// (createGoogle({headers})); replayed here with the same provider config
// (the main case suite builds a default provider and skips it).
func TestR82provaTSCaseProviderHeaders(t *testing.T) {
	var cases []tsfixture.Case
	for _, c := range tsfixture.LoadCases(t, "testdata/tscases/language.json.gz") {
		if c.Test == "should pass headers" {
			// The user agent assertion checks the TS test version
			// (ai-sdk-google/0.0.0-test); Go reports its own version, so it
			// becomes a prefix check.
			var asserts []tsfixture.Assertion
			for _, a := range c.Asserts {
				if a.Expr == "server.calls[0].requestUserAgent" && a.Arg == "ai-sdk-google/0.0.0-test" {
					a.Arg = "ai-sdk-google/"
				}
				asserts = append(asserts, a)
			}
			c.Asserts = asserts
			cases = append(cases, c)
		}
	}
	if len(cases) != 1 {
		t.Fatalf("found %d cases", len(cases))
	}
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base, Headers: map[string]string{"Custom-Provider-Header": "provider-header-value"}}).LanguageModel("gemini-pro")
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}
