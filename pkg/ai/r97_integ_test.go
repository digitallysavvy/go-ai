package ai

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR97integRerankProviderMetadataNonFinite (cr-r97-stream-to-integrator
// item 2; gen's duplicate CR): a reranking model's provider metadata holding
// +Inf (a provider's 1e400, ruling 4) reaches the finish event, written the
// JSON.stringify way (null); json.Marshal dropped it whole.
func TestR97integRerankProviderMetadataNonFinite(t *testing.T) {
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking:          []types.RerankItem{{Index: 0, RelevanceScore: 1.0}},
				ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"x": math.Inf(1), "k": "v"}},
			}, nil
		},
	}
	var got string
	_, err := Rerank(context.Background(), RerankOptions{
		Model: model, Documents: []string{"doc1"}, Query: "q",
		OnEnd: func(e RerankOnFinishEvent) { got = string(e.ProviderMetadata) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"p":{"k":"v","x":null}}` {
		t.Fatalf("event providerMetadata = %q, want {\"p\":{\"k\":\"v\",\"x\":null}}", got)
	}
}

// TestR97integSingleJSONParseError (class sweep "double-wrapped
// JSONParseError", prova97-03): TS safeParseJSON gives one JSONParseError
// whose cause is the SyntaxError. securejson already reports a __proto__ key
// as a JSONParseError, so wrapping it again nested two (the text repeated).
// Sites: the four Output parsers, generateObject's final parse and the tool
// call input.
func TestR97integSingleJSONParseError(t *testing.T) {
	ctx := context.Background()
	check := func(name string, err error) {
		t.Helper()
		var jpe *providererrors.JSONParseError
		if !errors.As(err, &jpe) {
			t.Errorf("%s: err = %v, want a JSONParseError", name, err)
			return
		}
		var inner *providererrors.JSONParseError
		if errors.As(jpe.Cause, &inner) || strings.Count(jpe.Error(), "JSON parsing failed") != 1 {
			t.Errorf("%s: nested JSONParseError: %v", name, jpe)
		}
	}
	js := JSONOutput(JSONOutputOptions{})
	_, err := js.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"__proto__":{}}`})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v", err)
	}
	check("json output", noObj.Cause)
	choice := ChoiceOutput[string](ChoiceOutputOptions[string]{Options: []string{"a"}})
	_, err = choice.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"result":"a","__proto__":{}}`})
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v", err)
	}
	check("choice output", noObj.Cause)
	_, err = toolCallInputArguments(types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"__proto__":{}}`})
	check("tool input", toolInputParseError(types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"__proto__":{}}`}, err))
}
