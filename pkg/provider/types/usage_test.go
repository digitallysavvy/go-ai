package types

import (
	"testing"
)

func TestUsage_Add(t *testing.T) {
	t.Parallel()

	input1, output1, total1 := int64(10), int64(5), int64(15)
	input2, output2, total2 := int64(20), int64(10), int64(30)
	u1 := Usage{InputTokens: &input1, OutputTokens: &output1, TotalTokens: &total1}
	u2 := Usage{InputTokens: &input2, OutputTokens: &output2, TotalTokens: &total2}

	result := u1.Add(u2)

	if result.InputTokens == nil || *result.InputTokens != 30 {
		t.Errorf("expected InputTokens 30, got %v", result.InputTokens)
	}
	if result.OutputTokens == nil || *result.OutputTokens != 15 {
		t.Errorf("expected OutputTokens 15, got %v", result.OutputTokens)
	}
	if result.TotalTokens == nil || *result.TotalTokens != 45 {
		t.Errorf("expected TotalTokens 45, got %v", result.TotalTokens)
	}
}

func TestUsage_Add_ZeroValues(t *testing.T) {
	t.Parallel()

	input1, output1, total1 := int64(10), int64(5), int64(15)
	u1 := Usage{InputTokens: &input1, OutputTokens: &output1, TotalTokens: &total1}
	u2 := Usage{}

	result := u1.Add(u2)

	if result.InputTokens == nil || *result.InputTokens != 10 {
		t.Errorf("expected InputTokens 10, got %v", result.InputTokens)
	}
	if result.OutputTokens == nil || *result.OutputTokens != 5 {
		t.Errorf("expected OutputTokens 5, got %v", result.OutputTokens)
	}
	if result.TotalTokens == nil || *result.TotalTokens != 15 {
		t.Errorf("expected TotalTokens 15, got %v", result.TotalTokens)
	}
}

func TestFinishReason_Constants(t *testing.T) {
	t.Parallel()

	if FinishReasonStop != "stop" {
		t.Errorf("expected 'stop', got %s", FinishReasonStop)
	}
	if FinishReasonLength != "length" {
		t.Errorf("expected 'length', got %s", FinishReasonLength)
	}
	if FinishReasonContentFilter != "content-filter" {
		t.Errorf("expected 'content-filter', got %s", FinishReasonContentFilter)
	}
	if FinishReasonToolCalls != "tool-calls" {
		t.Errorf("expected 'tool-calls', got %s", FinishReasonToolCalls)
	}
	if FinishReasonError != "error" {
		t.Errorf("expected 'error', got %s", FinishReasonError)
	}
	if FinishReasonOther != "other" {
		t.Errorf("expected 'other', got %s", FinishReasonOther)
	}
}

func TestWarning_Fields(t *testing.T) {
	t.Parallel()

	w := Warning{Type: "deprecation", Message: "This feature is deprecated"}

	if w.Type != "deprecation" {
		t.Errorf("expected Type 'deprecation', got %s", w.Type)
	}
	if w.Message != "This feature is deprecated" {
		t.Errorf("expected Message 'This feature is deprecated', got %s", w.Message)
	}
}

func TestEmbeddingUsage_Fields(t *testing.T) {
	t.Parallel()

	eu := EmbeddingUsage{InputTokens: 100, TotalTokens: 100}

	if eu.InputTokens != 100 {
		t.Errorf("expected InputTokens 100, got %d", eu.InputTokens)
	}
	if eu.TotalTokens != 100 {
		t.Errorf("expected TotalTokens 100, got %d", eu.TotalTokens)
	}
}

func TestImageUsage_Fields(t *testing.T) {
	t.Parallel()

	iu := ImageUsage{ImageCount: 3}

	if iu.ImageCount != 3 {
		t.Errorf("expected ImageCount 3, got %d", iu.ImageCount)
	}
}

func TestSpeechUsage_Fields(t *testing.T) {
	t.Parallel()

	su := SpeechUsage{CharacterCount: 500}

	if su.CharacterCount != 500 {
		t.Errorf("expected CharacterCount 500, got %d", su.CharacterCount)
	}
}

func TestTranscriptionUsage_Fields(t *testing.T) {
	t.Parallel()

	tu := TranscriptionUsage{DurationSeconds: 120.5}

	if tu.DurationSeconds != 120.5 {
		t.Errorf("expected DurationSeconds 120.5, got %f", tu.DurationSeconds)
	}
}

// Tests for new text/image token differentiation

func TestUsage_Add_WithInputDetails(t *testing.T) {
	t.Parallel()

	text1, image1 := int64(100), int64(500)
	text2, image2 := int64(50), int64(300)
	input1, output1 := int64(600), int64(50)
	input2, output2 := int64(350), int64(30)

	u1 := Usage{
		InputTokens:  &input1,
		OutputTokens: &output1,
		InputDetails: &InputTokenDetails{
			TextTokens:  &text1,
			ImageTokens: &image1,
		},
	}

	u2 := Usage{
		InputTokens:  &input2,
		OutputTokens: &output2,
		InputDetails: &InputTokenDetails{
			TextTokens:  &text2,
			ImageTokens: &image2,
		},
	}

	result := u1.Add(u2)

	if result.InputDetails == nil {
		t.Fatal("expected InputDetails to be non-nil")
	}
	if result.InputDetails.TextTokens == nil || *result.InputDetails.TextTokens != 150 {
		t.Errorf("expected TextTokens 150, got %v", result.InputDetails.TextTokens)
	}
	if result.InputDetails.ImageTokens == nil || *result.InputDetails.ImageTokens != 800 {
		t.Errorf("expected ImageTokens 800, got %v", result.InputDetails.ImageTokens)
	}
}

func TestUsage_Add_WithCacheAndTextImage(t *testing.T) {
	t.Parallel()

	text1, image1 := int64(100), int64(400)
	cache1 := int64(50)
	text2, image2 := int64(75), int64(250)
	cache2 := int64(25)

	input1, input2 := int64(500), int64(325)

	u1 := Usage{
		InputTokens: &input1,
		InputDetails: &InputTokenDetails{
			TextTokens:      &text1,
			ImageTokens:     &image1,
			CacheReadTokens: &cache1,
		},
	}

	u2 := Usage{
		InputTokens: &input2,
		InputDetails: &InputTokenDetails{
			TextTokens:      &text2,
			ImageTokens:     &image2,
			CacheReadTokens: &cache2,
		},
	}

	result := u1.Add(u2)

	if result.InputDetails == nil {
		t.Fatal("expected InputDetails to be non-nil")
	}
	if result.InputDetails.TextTokens == nil || *result.InputDetails.TextTokens != 175 {
		t.Errorf("expected TextTokens 175, got %v", result.InputDetails.TextTokens)
	}
	if result.InputDetails.ImageTokens == nil || *result.InputDetails.ImageTokens != 650 {
		t.Errorf("expected ImageTokens 650, got %v", result.InputDetails.ImageTokens)
	}
	if result.InputDetails.CacheReadTokens == nil || *result.InputDetails.CacheReadTokens != 75 {
		t.Errorf("expected CacheReadTokens 75, got %v", result.InputDetails.CacheReadTokens)
	}
}

func TestUsage_Add_WithNilTextImageTokens(t *testing.T) {
	t.Parallel()

	input1, input2 := int64(100), int64(200)
	u1 := Usage{InputTokens: &input1}
	u2 := Usage{
		InputTokens: &input2,
		InputDetails: &InputTokenDetails{
			TextTokens:  ptrInt64(50),
			ImageTokens: ptrInt64(150),
		},
	}

	result := u1.Add(u2)

	if result.InputDetails == nil {
		t.Fatal("expected InputDetails to be non-nil")
	}
	if result.InputDetails.TextTokens == nil || *result.InputDetails.TextTokens != 50 {
		t.Errorf("expected TextTokens 50, got %v", result.InputDetails.TextTokens)
	}
	if result.InputDetails.ImageTokens == nil || *result.InputDetails.ImageTokens != 150 {
		t.Errorf("expected ImageTokens 150, got %v", result.InputDetails.ImageTokens)
	}
}

func TestUsage_GetInputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage Usage
		want  int64
	}{
		{
			name:  "with value",
			usage: Usage{InputTokens: ptrInt64(100)},
			want:  100,
		},
		{
			name:  "nil pointer",
			usage: Usage{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.GetInputTokens()
			if got != tt.want {
				t.Errorf("GetInputTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUsage_GetOutputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage Usage
		want  int64
	}{
		{
			name:  "with value",
			usage: Usage{OutputTokens: ptrInt64(50)},
			want:  50,
		},
		{
			name:  "nil pointer",
			usage: Usage{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.GetOutputTokens()
			if got != tt.want {
				t.Errorf("GetOutputTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUsage_GetTotalTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage Usage
		want  int64
	}{
		{
			name:  "with value",
			usage: Usage{TotalTokens: ptrInt64(150)},
			want:  150,
		},
		{
			name:  "nil pointer",
			usage: Usage{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.GetTotalTokens()
			if got != tt.want {
				t.Errorf("GetTotalTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInputTokenDetails_TextAndImageTokens(t *testing.T) {
	t.Parallel()

	text, image := int64(100), int64(500)
	details := InputTokenDetails{
		TextTokens:  &text,
		ImageTokens: &image,
	}

	if details.TextTokens == nil || *details.TextTokens != 100 {
		t.Errorf("expected TextTokens 100, got %v", details.TextTokens)
	}
	if details.ImageTokens == nil || *details.ImageTokens != 500 {
		t.Errorf("expected ImageTokens 500, got %v", details.ImageTokens)
	}
}

func TestInputTokenDetails_AllFields(t *testing.T) {
	t.Parallel()

	noCache, cacheRead, cacheWrite := int64(100), int64(50), int64(25)
	text, image := int64(75), int64(100)

	details := InputTokenDetails{
		NoCacheTokens:    &noCache,
		CacheReadTokens:  &cacheRead,
		CacheWriteTokens: &cacheWrite,
		TextTokens:       &text,
		ImageTokens:      &image,
	}

	if details.NoCacheTokens == nil || *details.NoCacheTokens != 100 {
		t.Errorf("expected NoCacheTokens 100, got %v", details.NoCacheTokens)
	}
	if details.CacheReadTokens == nil || *details.CacheReadTokens != 50 {
		t.Errorf("expected CacheReadTokens 50, got %v", details.CacheReadTokens)
	}
	if details.CacheWriteTokens == nil || *details.CacheWriteTokens != 25 {
		t.Errorf("expected CacheWriteTokens 25, got %v", details.CacheWriteTokens)
	}
	if details.TextTokens == nil || *details.TextTokens != 75 {
		t.Errorf("expected TextTokens 75, got %v", details.TextTokens)
	}
	if details.ImageTokens == nil || *details.ImageTokens != 100 {
		t.Errorf("expected ImageTokens 100, got %v", details.ImageTokens)
	}
}

func TestUsage_Add_ComplexScenario(t *testing.T) {
	t.Parallel()

	// Scenario: Multimodal batch processing with caching
	u1 := Usage{
		InputTokens:  ptrInt64(1000),
		OutputTokens: ptrInt64(200),
		TotalTokens:  ptrInt64(1200),
		InputDetails: &InputTokenDetails{
			NoCacheTokens:    ptrInt64(800),
			CacheReadTokens:  ptrInt64(200),
			CacheWriteTokens: ptrInt64(100),
			TextTokens:       ptrInt64(100),
			ImageTokens:      ptrInt64(900),
		},
		OutputDetails: &OutputTokenDetails{
			TextTokens:      ptrInt64(180),
			ReasoningTokens: ptrInt64(20),
		},
	}

	u2 := Usage{
		InputTokens:  ptrInt64(500),
		OutputTokens: ptrInt64(100),
		TotalTokens:  ptrInt64(600),
		InputDetails: &InputTokenDetails{
			NoCacheTokens:    ptrInt64(300),
			CacheReadTokens:  ptrInt64(200),
			CacheWriteTokens: nil,
			TextTokens:       ptrInt64(50),
			ImageTokens:      ptrInt64(450),
		},
		OutputDetails: &OutputTokenDetails{
			TextTokens:      ptrInt64(100),
			ReasoningTokens: nil,
		},
	}

	result := u1.Add(u2)

	// Verify totals
	if *result.InputTokens != 1500 {
		t.Errorf("expected InputTokens 1500, got %d", *result.InputTokens)
	}
	if *result.OutputTokens != 300 {
		t.Errorf("expected OutputTokens 300, got %d", *result.OutputTokens)
	}
	if *result.TotalTokens != 1800 {
		t.Errorf("expected TotalTokens 1800, got %d", *result.TotalTokens)
	}

	// Verify input details
	if *result.InputDetails.NoCacheTokens != 1100 {
		t.Errorf("expected NoCacheTokens 1100, got %d", *result.InputDetails.NoCacheTokens)
	}
	if *result.InputDetails.CacheReadTokens != 400 {
		t.Errorf("expected CacheReadTokens 400, got %d", *result.InputDetails.CacheReadTokens)
	}
	if *result.InputDetails.CacheWriteTokens != 100 {
		t.Errorf("expected CacheWriteTokens 100, got %d", *result.InputDetails.CacheWriteTokens)
	}
	if *result.InputDetails.TextTokens != 150 {
		t.Errorf("expected TextTokens 150, got %d", *result.InputDetails.TextTokens)
	}
	if *result.InputDetails.ImageTokens != 1350 {
		t.Errorf("expected ImageTokens 1350, got %d", *result.InputDetails.ImageTokens)
	}

	// Verify output details
	if *result.OutputDetails.TextTokens != 280 {
		t.Errorf("expected output TextTokens 280, got %d", *result.OutputDetails.TextTokens)
	}
	if *result.OutputDetails.ReasoningTokens != 20 {
		t.Errorf("expected ReasoningTokens 20, got %d", *result.OutputDetails.ReasoningTokens)
	}
}

// Helper function
func ptrInt64(v int64) *int64 {
	return &v
}
