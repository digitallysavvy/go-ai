package openai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func requireInvalidOptions(t *testing.T, err error, name string) {
	t.Helper()
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v (%T), want InvalidArgumentError", err, err)
	}
	if invalid.Field != "providerOptions" || invalid.Message != "invalid "+name+" provider options" {
		t.Fatalf("InvalidArgumentError = %#v", invalid)
	}
	if invalid.Cause == nil {
		t.Fatalf("InvalidArgumentError has no cause")
	}
}

func TestChatProviderOptionsValidated(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	bad := []map[string]interface{}{
		{"reasoningEffort": "ultra"},
		{"user": 5},
		{"parallelToolCalls": "yes"},
		{"logprobs": "a"},
		{"logitBias": map[string]interface{}{"abc": 1}},
		{"logitBias": map[string]interface{}{"12": "x"}},
		{"metadata": map[string]interface{}{"k": 1}},
		{"metadata": map[string]interface{}{strings.Repeat("k", 65): "v"}},
		{"metadata": map[string]interface{}{"k": strings.Repeat("v", 513)}},
		{"serviceTier": "gold"},
		{"promptCacheOptions": map[string]interface{}{"ttl": "1h"}},
		{"store": nil},
		{"textVerbosity": "loud"},
		{"maxCompletionTokens": "10"},
	}
	for i, o := range bad {
		_, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"openai": o},
		}, false)
		if err == nil {
			t.Fatalf("case %d (%v): expected error", i, o)
		}
		requireInvalidOptions(t, err, "openai")
	}
	// A non-object value is rejected too.
	_, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": "high"},
	}, false)
	requireInvalidOptions(t, err, "openai")

	good := map[string]interface{}{
		"reasoningEffort": "high", "user": "u", "logprobs": true, "logitBias": map[string]interface{}{"12": 1, "": 2},
		"metadata": map[string]interface{}{"k": "v"}, "serviceTier": "auto", "unknownKey": 1,
		"promptCacheOptions": map[string]interface{}{"mode": "implicit", "ttl": "30m"},
	}
	if _, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": good},
	}, false); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

func TestCompatChatProviderOptionsValidated(t *testing.T) {
	p := New(Config{APIKey: "k", Name: "acme", OpenAICompatible: true})
	m := NewLanguageModel(p, "m")
	_, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"acme": map[string]interface{}{"strictJsonSchema": "yes"}},
	}, false)
	requireInvalidOptions(t, err, "acme")
	// Options the compat schema does not declare pass through untouched.
	if _, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"acme": map[string]interface{}{"reasoningEffort": "whatever", "custom": 1}},
	}, false); err != nil {
		t.Fatalf("valid compat options rejected: %v", err)
	}
}

func TestCompletionProviderOptionsValidated(t *testing.T) {
	m := NewCompletionModel(New(Config{APIKey: "k"}), "gpt-3.5-turbo-instruct")
	for _, o := range []map[string]interface{}{{"echo": "yes"}, {"suffix": 1}, {"logitBias": map[string]interface{}{"1": "x"}}, {"logprobs": "2"}} {
		_, _, err := m.buildCompletionRequest(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"openai": o},
		}, false)
		requireInvalidOptions(t, err, "openai")
	}
}

func TestEmbeddingProviderOptionsValidated(t *testing.T) {
	m := NewEmbeddingModel(New(Config{APIKey: "k"}), "text-embedding-3-small")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"dimensions": "256"}}}
	_, err := m.DoEmbed(context.Background(), "x", opts)
	requireInvalidOptions(t, err, "openai")
	_, err = m.DoEmbedMany(context.Background(), []string{"x"}, opts)
	requireInvalidOptions(t, err, "openai")

	// OpenAI-compatible: validation runs before the too-many-values check.
	cm := NewEmbeddingModel(New(Config{APIKey: "k", Name: "acme", OpenAICompatible: true}), "e")
	copts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"acme": map[string]interface{}{"user": 1}}}
	many := make([]string, cm.MaxEmbeddingsPerCall()+1)
	_, err = cm.DoEmbedMany(context.Background(), many, copts)
	requireInvalidOptions(t, err, "acme")
}
