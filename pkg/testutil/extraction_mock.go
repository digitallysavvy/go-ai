package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// MockExtractionModel is a mock provider.ExtractionModel for testing. It
// mirrors TypeScript's Experimental_ExtractionMockModelV4.
type MockExtractionModel struct {
	DoExtractFunc func(ctx context.Context, opts provider.ExtractionCallOptions) (*provider.ExtractionResult, error)
	ProviderName  string
	ModelName     string

	// FieldTypes overrides the supported field types; nil means all seven.
	FieldTypes []string

	mu           sync.Mutex
	ExtractCalls []provider.ExtractionCallOptions
}

func (m *MockExtractionModel) SpecificationVersion() string { return "v4" }

func (m *MockExtractionModel) Provider() string {
	if m.ProviderName == "" {
		return "mock-provider"
	}
	return m.ProviderName
}

func (m *MockExtractionModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-model-id"
	}
	return m.ModelName
}

func (m *MockExtractionModel) SupportedFieldTypes() []string {
	if m.FieldTypes != nil {
		return m.FieldTypes
	}
	return provider.ExtractionFieldTypes()
}

// DoExtract records the call and runs DoExtractFunc; without one it fails
// like the TS notImplemented default.
func (m *MockExtractionModel) DoExtract(ctx context.Context, opts provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
	m.mu.Lock()
	m.ExtractCalls = append(m.ExtractCalls, opts)
	m.mu.Unlock()
	if m.DoExtractFunc != nil {
		return m.DoExtractFunc(ctx, opts)
	}
	return nil, errors.New("Not implemented")
}
