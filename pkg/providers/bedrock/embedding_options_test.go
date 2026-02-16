package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

func TestCohereEmbeddingOptionsValidation(t *testing.T) {
	tests := []struct {
		name      string
		options   CohereEmbeddingOptions
		wantError bool
	}{
		{
			name: "valid 256 dimension",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension256),
				InputType:       cohere.InputTypeSearchQuery,
			},
			wantError: false,
		},
		{
			name: "valid 512 dimension",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension512),
				InputType:       cohere.InputTypeSearchDocument,
			},
			wantError: false,
		},
		{
			name: "valid 1024 dimension",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension1024),
				InputType:       cohere.InputTypeClustering,
			},
			wantError: false,
		},
		{
			name: "valid 1536 dimension",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension1536),
				InputType:       cohere.InputTypeClassification,
			},
			wantError: false,
		},
		{
			name: "nil dimension",
			options: CohereEmbeddingOptions{
				InputType: cohere.InputTypeSearchQuery,
			},
			wantError: false,
		},
		{
			name: "invalid dimension",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.OutputDimension(384)),
				InputType:       cohere.InputTypeSearchQuery,
			},
			wantError: true,
		},
		{
			name: "invalid input type",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension512),
				InputType:       "invalid",
			},
			wantError: true,
		},
		{
			name: "valid with truncate",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension1024),
				InputType:       cohere.InputTypeSearchQuery,
				Truncate:        cohere.TruncateEnd,
			},
			wantError: false,
		},
		{
			name: "invalid truncate",
			options: CohereEmbeddingOptions{
				OutputDimension: ptr(cohere.Dimension512),
				InputType:       cohere.InputTypeSearchQuery,
				Truncate:        "MIDDLE",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.options.Validate()

			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDefaultCohereEmbeddingOptions(t *testing.T) {
	opts := DefaultCohereEmbeddingOptions()

	if opts.InputType != cohere.InputTypeSearchQuery {
		t.Errorf("DefaultCohereEmbeddingOptions() InputType = %v, want %v", opts.InputType, cohere.InputTypeSearchQuery)
	}

	if opts.OutputDimension != nil {
		t.Errorf("DefaultCohereEmbeddingOptions() OutputDimension = %v, want nil", opts.OutputDimension)
	}

	if opts.Truncate != "" {
		t.Errorf("DefaultCohereEmbeddingOptions() Truncate = %v, want empty", opts.Truncate)
	}

	// Validate default options should pass
	if err := opts.Validate(); err != nil {
		t.Errorf("DefaultCohereEmbeddingOptions() validation failed: %v", err)
	}
}

// Helper function to create pointer to OutputDimension
func ptr(d cohere.OutputDimension) *cohere.OutputDimension {
	return &d
}
