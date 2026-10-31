package types

import (
	"slices"
	"testing"
)

func TestReasoningLevelConstantValues(t *testing.T) {
	got := []ReasoningLevel{
		ReasoningDefault,
		ReasoningNone,
		ReasoningMinimal,
		ReasoningLow,
		ReasoningMedium,
		ReasoningHigh,
		ReasoningXHigh,
		ReasoningMax,
	}
	want := []ReasoningLevel{
		"provider-default",
		"none",
		"minimal",
		"low",
		"medium",
		"high",
		"xhigh",
		"max",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("reasoning constants mismatch\n got: %v\nwant: %v", got, want)
	}
}
