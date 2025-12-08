package registry

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.providers == nil {
		t.Error("expected providers map to be initialized")
	}
	if r.aliases == nil {
		t.Error("expected aliases map to be initialized")
	}
}

func TestRegistry_RegisterProvider(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	p := &testutil.MockProvider{ProviderName: "test-provider"}

	r.RegisterProvider("test", p)

	// Should be able to get the provider back
	retrieved, err := r.GetProvider("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != p {
		t.Error("expected same provider to be returned")
	}
}

func TestRegistry_GetProvider_NotFound(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	_, err := r.GetProvider("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestRegistry_RegisterAlias(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	p := &testutil.MockProvider{ProviderName: "openai"}
	r.RegisterProvider("openai", p)

	r.RegisterAlias("gpt-4", "openai:gpt-4")

	// Should be able to resolve the alias
	model, err := r.ResolveLanguageModel("gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
}

func TestRegistry_ResolveLanguageModel_Direct(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	p := &testutil.MockProvider{ProviderName: "openai"}
	r.RegisterProvider("openai", p)

	model, err := r.ResolveLanguageModel("openai:gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
	if model.ModelID() != "gpt-4" {
		t.Errorf("expected model ID 'gpt-4', got %s", model.ModelID())
	}
}

func TestRegistry_ResolveLanguageModel_ProviderNotFound(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	_, err := r.ResolveLanguageModel("nonexistent:model")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestRegistry_ResolveLanguageModel_InvalidFormat(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	_, err := r.ResolveLanguageModel("invalid-format")
	if err == nil {
		t.Error("expected error for invalid model string format")
	}
}

func TestRegistry_ResolveEmbeddingModel_Direct(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	p := &testutil.MockProvider{ProviderName: "openai"}
	r.RegisterProvider("openai", p)

	model, err := r.ResolveEmbeddingModel("openai:text-embedding-ada-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
	if model.ModelID() != "text-embedding-ada-002" {
		t.Errorf("expected model ID 'text-embedding-ada-002', got %s", model.ModelID())
	}
}

func TestRegistry_ResolveEmbeddingModel_WithAlias(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	p := &testutil.MockProvider{ProviderName: "openai"}
	r.RegisterProvider("openai", p)

	r.RegisterAlias("ada", "openai:text-embedding-ada-002")

	model, err := r.ResolveEmbeddingModel("ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
}

func TestRegistry_ResolveEmbeddingModel_ProviderNotFound(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	_, err := r.ResolveEmbeddingModel("nonexistent:model")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestRegistry_ListProviders(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})
	r.RegisterProvider("anthropic", &testutil.MockProvider{ProviderName: "anthropic"})

	providers := r.ListProviders()

	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}

	// Check that both providers are in the list
	foundOpenai := false
	foundAnthropic := false
	for _, p := range providers {
		if p == "openai" {
			foundOpenai = true
		}
		if p == "anthropic" {
			foundAnthropic = true
		}
	}
	if !foundOpenai {
		t.Error("expected 'openai' in providers list")
	}
	if !foundAnthropic {
		t.Error("expected 'anthropic' in providers list")
	}
}

func TestRegistry_ListAliases(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterAlias("gpt-4", "openai:gpt-4")
	r.RegisterAlias("claude", "anthropic:claude-3-opus")

	aliases := r.ListAliases()

	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases["gpt-4"] != "openai:gpt-4" {
		t.Errorf("expected alias 'gpt-4' to map to 'openai:gpt-4', got %s", aliases["gpt-4"])
	}
	if aliases["claude"] != "anthropic:claude-3-opus" {
		t.Errorf("expected alias 'claude' to map to 'anthropic:claude-3-opus', got %s", aliases["claude"])
	}
}

func TestParseModelString_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input          string
		expectProvider string
		expectModel    string
	}{
		{"openai:gpt-4", "openai", "gpt-4"},
		{"anthropic:claude-3-opus", "anthropic", "claude-3-opus"},
		{"provider:model-with-dashes", "provider", "model-with-dashes"},
		{"a:b", "a", "b"},
	}

	for _, tt := range tests {
		provider, modelID, err := parseModelString(tt.input)
		if err != nil {
			t.Errorf("parseModelString(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if provider != tt.expectProvider {
			t.Errorf("parseModelString(%q) provider = %q, want %q", tt.input, provider, tt.expectProvider)
		}
		if modelID != tt.expectModel {
			t.Errorf("parseModelString(%q) modelID = %q, want %q", tt.input, modelID, tt.expectModel)
		}
	}
}

func TestParseModelString_Invalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"no-colon",
		"",
		"justmodel",
	}

	for _, input := range tests {
		_, _, err := parseModelString(input)
		if err == nil {
			t.Errorf("parseModelString(%q) expected error, got nil", input)
		}
	}
}

func TestParseModelString_EmptyParts(t *testing.T) {
	t.Parallel()

	// Test edge cases with colons
	provider, modelID, err := parseModelString(":model")
	if err != nil {
		t.Errorf("parseModelString(':model') unexpected error: %v", err)
	}
	if provider != "" {
		t.Errorf("expected empty provider, got %q", provider)
	}
	if modelID != "model" {
		t.Errorf("expected modelID 'model', got %q", modelID)
	}

	provider, modelID, err = parseModelString("provider:")
	if err != nil {
		t.Errorf("parseModelString('provider:') unexpected error: %v", err)
	}
	if provider != "provider" {
		t.Errorf("expected provider 'provider', got %q", provider)
	}
	if modelID != "" {
		t.Errorf("expected empty modelID, got %q", modelID)
	}
}

// Global registry tests

func TestGlobalRegistry_RegisterProvider(t *testing.T) {
	// Note: This test modifies global state, so we can't run it in parallel
	// with other global registry tests

	p := &testutil.MockProvider{ProviderName: "global-test"}
	RegisterProvider("global-test", p)

	retrieved, err := GetProvider("global-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved.Name() != "global-test" {
		t.Errorf("expected provider name 'global-test', got %s", retrieved.Name())
	}
}

func TestGlobalRegistry_RegisterAlias(t *testing.T) {
	// Setup: register provider first
	p := &testutil.MockProvider{ProviderName: "alias-provider"}
	RegisterProvider("alias-provider", p)

	RegisterAlias("my-model", "alias-provider:the-model")

	model, err := ResolveLanguageModel("my-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
}

func TestGlobalRegistry_ResolveEmbeddingModel(t *testing.T) {
	// Setup: register provider first
	p := &testutil.MockProvider{ProviderName: "embed-provider"}
	RegisterProvider("embed-provider", p)

	model, err := ResolveEmbeddingModel("embed-provider:embed-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Error("expected non-nil model")
	}
}

func TestGetGlobalRegistry(t *testing.T) {
	t.Parallel()

	r := GetGlobalRegistry()

	if r == nil {
		t.Error("expected non-nil global registry")
	}
}

func TestRegistry_ProviderError(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	expectedErr := errors.New("model not found")

	p := &testutil.MockProvider{
		ProviderName: "error-provider",
		LanguageModelFunc: func(modelID string) (provider.LanguageModel, error) {
			return nil, expectedErr
		},
	}
	r.RegisterProvider("error-provider", p)

	_, err := r.ResolveLanguageModel("error-provider:nonexistent")
	if err == nil {
		t.Error("expected error from provider")
	}
}

func TestRegistry_OverwriteProvider(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	p1 := &testutil.MockProvider{ProviderName: "provider-v1"}
	p2 := &testutil.MockProvider{ProviderName: "provider-v2"}

	r.RegisterProvider("test", p1)
	r.RegisterProvider("test", p2) // Overwrite

	retrieved, err := r.GetProvider("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the second provider
	if retrieved.Name() != "provider-v2" {
		t.Errorf("expected provider 'provider-v2', got %s", retrieved.Name())
	}
}

func TestRegistry_OverwriteAlias(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	r.RegisterAlias("model", "provider1:model1")
	r.RegisterAlias("model", "provider2:model2") // Overwrite

	aliases := r.ListAliases()
	if aliases["model"] != "provider2:model2" {
		t.Errorf("expected alias to be overwritten to 'provider2:model2', got %s", aliases["model"])
	}
}

func TestRegistry_EmptyListProviders(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	providers := r.ListProviders()

	if len(providers) != 0 {
		t.Errorf("expected empty providers list, got %d", len(providers))
	}
}

func TestRegistry_EmptyListAliases(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	aliases := r.ListAliases()

	if len(aliases) != 0 {
		t.Errorf("expected empty aliases map, got %d", len(aliases))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	// Concurrent provider registration
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(i int) {
			p := &testutil.MockProvider{ProviderName: "concurrent"}
			r.RegisterProvider("concurrent", p)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func() {
			_, _ = r.GetProvider("concurrent")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestRegistry_ListAliasesReturnsACopy(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterAlias("original", "provider:model")

	aliases := r.ListAliases()
	aliases["modified"] = "should-not-affect-registry"

	// The registry should not be modified
	registryAliases := r.ListAliases()
	if _, ok := registryAliases["modified"]; ok {
		t.Error("modifying returned aliases map should not affect registry")
	}
}
