package cohere

import (
	"testing"
)

func TestOutputDimensionValidation(t *testing.T) {
	tests := []struct {
		name      string
		dimension *OutputDimension
		wantError bool
	}{
		{
			name:      "nil dimension",
			dimension: nil,
			wantError: false,
		},
		{
			name:      "256 dimension",
			dimension: ptr(Dimension256),
			wantError: false,
		},
		{
			name:      "512 dimension",
			dimension: ptr(Dimension512),
			wantError: false,
		},
		{
			name:      "1024 dimension",
			dimension: ptr(Dimension1024),
			wantError: false,
		},
		{
			name:      "1536 dimension",
			dimension: ptr(Dimension1536),
			wantError: false,
		},
		{
			name:      "invalid dimension 384",
			dimension: ptr(OutputDimension(384)),
			wantError: true,
		},
		{
			name:      "invalid dimension 128",
			dimension: ptr(OutputDimension(128)),
			wantError: true,
		},
		{
			name:      "invalid dimension 2048",
			dimension: ptr(OutputDimension(2048)),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := EmbeddingOptions{
				OutputDimension: tt.dimension,
			}

			err := opts.Validate()

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

func TestInputTypeValidation(t *testing.T) {
	tests := []struct {
		name      string
		inputType InputType
		wantError bool
	}{
		{
			name:      "empty input type",
			inputType: "",
			wantError: false,
		},
		{
			name:      "search_document",
			inputType: InputTypeSearchDocument,
			wantError: false,
		},
		{
			name:      "search_query",
			inputType: InputTypeSearchQuery,
			wantError: false,
		},
		{
			name:      "classification",
			inputType: InputTypeClassification,
			wantError: false,
		},
		{
			name:      "clustering",
			inputType: InputTypeClustering,
			wantError: false,
		},
		{
			name:      "invalid input type",
			inputType: "invalid",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := EmbeddingOptions{
				InputType: tt.inputType,
			}

			err := opts.Validate()

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

func TestTruncateModeValidation(t *testing.T) {
	tests := []struct {
		name      string
		truncate  TruncateMode
		wantError bool
	}{
		{
			name:      "empty truncate",
			truncate:  "",
			wantError: false,
		},
		{
			name:      "NONE",
			truncate:  TruncateNone,
			wantError: false,
		},
		{
			name:      "START",
			truncate:  TruncateStart,
			wantError: false,
		},
		{
			name:      "END",
			truncate:  TruncateEnd,
			wantError: false,
		},
		{
			name:      "invalid truncate",
			truncate:  "MIDDLE",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := EmbeddingOptions{
				Truncate: tt.truncate,
			}

			err := opts.Validate()

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

func TestDefaultEmbeddingOptions(t *testing.T) {
	opts := DefaultEmbeddingOptions()

	if opts.InputType != InputTypeSearchDocument {
		t.Errorf("DefaultEmbeddingOptions() InputType = %v, want %v", opts.InputType, InputTypeSearchDocument)
	}

	if opts.OutputDimension != nil {
		t.Errorf("DefaultEmbeddingOptions() OutputDimension = %v, want nil", opts.OutputDimension)
	}

	if opts.Truncate != "" {
		t.Errorf("DefaultEmbeddingOptions() Truncate = %v, want empty", opts.Truncate)
	}

	// Validate default options should pass
	if err := opts.Validate(); err != nil {
		t.Errorf("DefaultEmbeddingOptions() validation failed: %v", err)
	}
}

// Helper function to create pointer to OutputDimension
func ptr(d OutputDimension) *OutputDimension {
	return &d
}
