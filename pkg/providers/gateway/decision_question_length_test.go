package gateway

import (
	"strings"
	"testing"
)

// zod .max(256) counts UTF-16 code units, not bytes.
func TestDecisionCondition_QuestionLengthCountsUTF16Units(t *testing.T) {
	q := strings.Repeat("字", 256) // 768 bytes, 256 UTF-16 units
	if err := validateDirectCondition(map[string]interface{}{"question": q, "confidenceBelow": 0.5}); err != nil {
		t.Fatalf("256-unit question should be accepted: %v", err)
	}
	if err := validateDirectCondition(map[string]interface{}{"question": q + "字", "confidenceBelow": 0.5}); err == nil {
		t.Fatal("257-unit question should be rejected")
	}
}
