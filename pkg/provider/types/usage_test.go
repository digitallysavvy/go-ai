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
