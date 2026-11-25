package testutil

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestPortCarryMockExtractionModel(t *testing.T) {
	var _ provider.ExtractionModel = (*MockExtractionModel)(nil)
	m := &MockExtractionModel{}
	if m.Provider() != "mock-provider" || m.ModelID() != "mock-model-id" || len(m.SupportedFieldTypes()) != 7 {
		t.Fatal("defaults")
	}
	if _, err := m.DoExtract(context.Background(), provider.ExtractionCallOptions{}); err == nil || err.Error() != "not implemented" {
		t.Fatalf("err = %v", err)
	}
	if len(m.ExtractCalls) != 1 {
		t.Fatal("call not recorded")
	}
}
