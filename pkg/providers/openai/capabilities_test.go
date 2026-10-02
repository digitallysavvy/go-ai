package openai

import "testing"

// TestGetLanguageModelCapabilitiesIsReasoningModel ports
// openai-language-model-capabilities.test.ts's isReasoningModel matrix.
func TestGetLanguageModelCapabilitiesIsReasoningModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"gpt-4.1", false},
		{"gpt-4.1-2025-04-14", false},
		{"gpt-4.1-mini", false},
		{"gpt-4.1-mini-2025-04-14", false},
		{"gpt-4.1-nano", false},
		{"gpt-4.1-nano-2025-04-14", false},
		{"gpt-4o", false},
		{"gpt-4o-2024-05-13", false},
		{"gpt-4o-2024-08-06", false},
		{"gpt-4o-2024-11-20", false},
		{"gpt-4o-audio-preview", false},
		{"gpt-4o-audio-preview-2024-12-17", false},
		{"gpt-4o-search-preview", false},
		{"gpt-4o-search-preview-2025-03-11", false},
		{"gpt-4o-mini-search-preview", false},
		{"gpt-4o-mini-search-preview-2025-03-11", false},
		{"gpt-4o-mini", false},
		{"gpt-4o-mini-2024-07-18", false},
		{"gpt-3.5-turbo-0125", false},
		{"gpt-3.5-turbo", false},
		{"gpt-3.5-turbo-1106", false},
		{"gpt-5-chat-latest", false},
		{"gpt-5.99-chat-latest", true},
		{"o1", true},
		{"o1-2024-12-17", true},
		{"o3-mini", true},
		{"o3-mini-2025-01-31", true},
		{"o3", true},
		{"o3-2025-04-16", true},
		{"o4", true},
		{"o4-mini", true},
		{"o4-mini-2025-04-16", true},
		{"o99", true},
		{"o99-2099-01-01", true},
		{"gpt-5", true},
		{"gpt-5-2025-08-07", true},
		{"gpt-5-codex", true},
		{"gpt-5-mini", true},
		{"gpt-5-mini-2025-08-07", true},
		{"gpt-5-nano", true},
		{"gpt-5-nano-2025-08-07", true},
		{"gpt-5-pro", true},
		{"gpt-5-pro-2025-10-06", true},
		{"gpt-5.4-mini", true},
		{"gpt-5.4-mini-2026-03-17", true},
		{"gpt-5.4-nano", true},
		{"gpt-5.4-nano-2026-03-17", true},
		{"gpt-5.5", true},
		{"gpt-5.5-2026-04-23", true},
		{"gpt-5.6", true},
		{"gpt-5.6-luna", true},
		{"gpt-5.6-sol", true},
		{"gpt-5.6-terra", true},
		{"gpt-5.99", true},
		{"gpt-6-astra", true},
		{"gpt-99", true},
		{"gpt-99-mini", true},
		{"new-unknown-model", false},
		{"ft:gpt-4o-2024-08-06:org:custom:abc123", false},
		{"ft:gpt-99:org:custom:abc123", false},
		{"acme-gpt-99-proxy", false},
		{"custom-model", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).IsReasoningModel
			if got != tt.want {
				t.Errorf("IsReasoningModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestGetLanguageModelCapabilitiesSupportsNonReasoningParameters(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"gpt-5.1", true},
		{"gpt-5.1-chat-latest", true},
		{"gpt-5.1-codex-mini", true},
		{"gpt-5.1-codex", true},
		{"gpt-5.2", true},
		{"gpt-5.2-pro", true},
		{"gpt-5.2-chat-latest", true},
		{"gpt-5.3-chat-latest", true},
		{"gpt-5.4", true},
		{"gpt-5.4-mini", true},
		{"gpt-5.4-nano", true},
		{"gpt-5.4-pro", true},
		{"gpt-5.4-2026-03-05", true},
		{"gpt-5.4-mini-2026-03-17", true},
		{"gpt-5.4-nano-2026-03-17", true},
		{"gpt-5.5", true},
		{"gpt-5.5-2026-04-23", true},
		{"gpt-5.6", true},
		{"gpt-5.6-luna", true},
		{"gpt-5.6-sol", true},
		{"gpt-5.6-terra", true},
		{"gpt-5.99", true},
		{"gpt-5.100", true},
		{"gpt-6-astra", false},
		{"gpt-99", false},
		{"gpt-5", false},
		{"gpt-5.0", false},
		{"gpt-5-mini", false},
		{"gpt-5-nano", false},
		{"gpt-5-pro", false},
		{"gpt-5-chat-latest", false},
		{"ft:gpt-99:org:custom:abc123", false},
		{"acme-gpt-99-proxy", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportsNonReasoningParameters
			if got != tt.want {
				t.Errorf("SupportsNonReasoningParameters(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestGetLanguageModelCapabilitiesGpt6AndLater(t *testing.T) {
	asyncTests := []struct {
		modelID string
		want    bool
	}{
		{"gpt-5.6", false},
		{"gpt-6-astra", true},
		{"gpt-6.1", true},
		{"gpt-7", true},
		{"gpt-99", true},
		{"custom-model", false},
	}
	for _, tt := range asyncTests {
		t.Run("async/"+tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportsAsyncToolCalling
			if got != tt.want {
				t.Errorf("SupportsAsyncToolCalling(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}

	configTests := []struct {
		modelID string
		want    bool
	}{
		{"gpt-5.6", false},
		{"gpt-6-astra", true},
		{"gpt-6.1", true},
		{"gpt-99", true},
	}
	for _, tt := range configTests {
		t.Run("config/"+tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportsConfigurationUpdate
			if got != tt.want {
				t.Errorf("SupportsConfigurationUpdate(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}

	effortTests := []struct {
		modelID string
		want    []string
	}{
		{"gpt-5.6", nil},
		{"gpt-6-astra", []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-99", []string{"low", "medium", "high", "xhigh", "max"}},
	}
	for _, tt := range effortTests {
		t.Run("efforts/"+tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportedReasoningEfforts
			if !stringSlicesEqual(got, tt.want) {
				t.Errorf("SupportedReasoningEfforts(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

// TestGetLanguageModelCapabilitiesGpt61Sol ports TS's "supports the
// documented GPT-6.1 Sol capabilities" test (commit 040033b608, #21747).
// gpt-6.1-sol is a plain GPT-6+ model (not the gpt-6-sol/gpt-6-luna special
// case), so its SupportedReasoningEfforts must NOT include "none".
func TestGetLanguageModelCapabilitiesGpt61Sol(t *testing.T) {
	got := GetLanguageModelCapabilities(ModelGPT61Sol)
	want := LanguageModelCapabilities{
		IsReasoningModel:               true,
		SystemMessageMode:              "developer",
		SupportsFlexProcessing:         true,
		SupportsPriorityProcessing:     true,
		SupportsConfigurationUpdate:    true,
		SupportsAsyncToolCalling:       true,
		SupportedReasoningEfforts:      []string{"low", "medium", "high", "xhigh", "max"},
		SupportsNonReasoningParameters: false,
	}
	if got.IsReasoningModel != want.IsReasoningModel ||
		got.SystemMessageMode != want.SystemMessageMode ||
		got.SupportsFlexProcessing != want.SupportsFlexProcessing ||
		got.SupportsPriorityProcessing != want.SupportsPriorityProcessing ||
		got.SupportsConfigurationUpdate != want.SupportsConfigurationUpdate ||
		got.SupportsAsyncToolCalling != want.SupportsAsyncToolCalling ||
		!stringSlicesEqual(got.SupportedReasoningEfforts, want.SupportedReasoningEfforts) ||
		got.SupportsNonReasoningParameters != want.SupportsNonReasoningParameters {
		t.Errorf("GetLanguageModelCapabilities(%q) = %+v, want %+v", ModelGPT61Sol, got, want)
	}
}

func TestGetLanguageModelCapabilitiesSupportsFlexProcessing(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"o1", false},
		{"o3", true},
		{"o4", true},
		{"o99", true},
		{"gpt-4.1", false},
		{"gpt-5", true},
		{"gpt-5.99", true},
		{"gpt-6-astra", true},
		{"gpt-99", true},
		{"gpt-99-chat-latest", false},
		{"ft:gpt-99:org:custom:abc123", false},
		{"acme-gpt-99-proxy", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportsFlexProcessing
			if got != tt.want {
				t.Errorf("SupportsFlexProcessing(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestGetLanguageModelCapabilitiesSupportsPriorityProcessing(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"o1", false},
		{"o3", true},
		{"o4", true},
		{"o99", true},
		{"gpt-4.1", true},
		{"gpt-5", true},
		{"gpt-5.4-nano", false},
		{"gpt-5.99", true},
		{"gpt-6-astra", true},
		{"gpt-99", true},
		{"gpt-99-nano", false},
		{"gpt-99-chat-latest", false},
		{"ft:gpt-99:org:custom:abc123", false},
		{"acme-gpt-99-proxy", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetLanguageModelCapabilities(tt.modelID).SupportsPriorityProcessing
			if got != tt.want {
				t.Errorf("SupportsPriorityProcessing(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
