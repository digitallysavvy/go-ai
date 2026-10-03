package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestSummarize(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "  Go is a compiled language.  ",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	got, err := Summarize(context.Background(), model, "long text")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Go is a compiled language." {
		t.Errorf("got %q", got)
	}

	// The mock records every call, so you can check what the model was sent.
	if len(model.GenerateCalls) != 1 {
		t.Fatalf("calls = %d, want 1", len(model.GenerateCalls))
	}
	if !strings.Contains(promptText(model.GenerateCalls[0]), "long text") {
		t.Error("prompt did not contain the input text")
	}
}

func TestSummarizeError(t *testing.T) {
	boom := errors.New("provider unavailable")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, boom
		},
	}
	if _, err := Summarize(context.Background(), model, "x"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func promptText(opts *provider.GenerateOptions) string {
	var b strings.Builder
	for _, m := range opts.Prompt.Messages {
		for _, p := range m.Content {
			if tc, ok := p.(types.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
	}
	return b.String()
}
