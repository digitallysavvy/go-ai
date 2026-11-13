package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Ruling 4 (round 96, stream CR 2): the finish-chunk provider metadata on
// LanguageModelCallEndEvent reads 1e400 as +Inf (TS JSON.parse), instead of
// dropping the whole map.
func TestR96integLanguageModelCallEndMetadataInf(t *testing.T) {
	md := json.RawMessage(`{"p":{"x":1e400}}`)
	var mu sync.Mutex
	var got interface{}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "t"},
			{Type: provider.ChunkTypeText, ID: "t", Text: "hi"},
			{Type: provider.ChunkTypeTextEnd, ID: "t"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, ProviderMetadata: md},
		}),
		Prompt: "x",
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			defer mu.Unlock()
			if p, ok := e.ProviderMetadata["p"].(map[string]interface{}); ok {
				got = p["x"]
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if x, _ := got.(float64); !math.IsInf(x, 1) {
		t.Fatalf("OnLanguageModelCallEnd providerMetadata p.x = %#v, want +Inf", got)
	}
}

// gen CR 2 (round 96): TS output.ts parses complete outputs with
// safeParseJSON (secure-json-parse), so a __proto__ key fails the parse:
// NoObjectGeneratedError "could not parse the response." with a
// JSONParseError cause.
func TestR96integOutputsRejectForbiddenPrototype(t *testing.T) {
	ctx := context.Background()
	obj := ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})})
	arr := ArrayOutput[string](ArrayOutputOptions[string]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"})})
	choice := ChoiceOutput[string](ChoiceOutputOptions[string]{Options: []string{"a", "b"}})
	js := JSONOutput(JSONOutputOptions{})
	cases := map[string]struct {
		text  string
		parse func(string) error
	}{
		"object": {`{"name":"x","__proto__":{}}`, func(s string) error {
			_, err := obj.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: s})
			return err
		}},
		"array": {`{"elements":["a"],"__proto__":{"x":1}}`, func(s string) error {
			_, err := arr.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: s})
			return err
		}},
		"choice": {`{"result":"a","constructor":{"prototype":{}}}`, func(s string) error {
			_, err := choice.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: s})
			return err
		}},
		"json": {`{"a":{"__proto__":{}}}`, func(s string) error {
			_, err := js.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: s})
			return err
		}},
	}
	for name, tc := range cases {
		err := tc.parse(tc.text)
		var noObj *NoObjectGeneratedError
		if !errors.As(err, &noObj) || noObj.Message != "No object generated: could not parse the response." {
			t.Errorf("%s: err = %v, want could not parse", name, err)
			continue
		}
		var jpe *providererrors.JSONParseError
		if !errors.As(noObj.Cause, &jpe) || !errors.Is(noObj.Cause, securejson.ErrForbiddenPrototype) {
			t.Errorf("%s: cause = %T %v, want JSONParseError with the forbidden prototype error", name, noObj.Cause, noObj.Cause)
		}
	}
	// A number beyond the double range reads as +Inf (JSON.parse).
	v, err := js.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"a":1e400}`})
	if m, _ := v.(map[string]interface{}); err != nil || !math.IsInf(m["a"].(float64), 1) {
		t.Fatalf("json output = %#v, %v, want a = +Inf", v, err)
	}
}

// gen CR 2: TS parse-tool-call.ts safeParseJSON rejects a tool input with a
// __proto__ key, so the call is an InvalidToolInputError.
func TestR96integToolInputForbiddenPrototype(t *testing.T) {
	_, err := toolCallInputArguments(types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"a":1,"__proto__":{}}`})
	if !errors.Is(err, securejson.ErrForbiddenPrototype) {
		t.Fatalf("err = %v, want the forbidden prototype error", err)
	}
	res, gerr := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{"value":1,"__proto__":{}}`}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if gerr != nil {
		t.Fatal(gerr)
	}
	tes := r73genToolErrors(res.Steps[0].Content)
	var target *InvalidToolInputError
	if len(tes) != 1 || !errors.As(tes[0].Err, &target) {
		t.Fatalf("step 0 content = %#v, want one InvalidToolInputError", res.Steps[0].Content)
	}
}
