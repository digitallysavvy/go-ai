package gemini

import "testing"

// TestGetModelPath mirrors TS get-model-path.test.ts's three cases for
// getModelPath: a model ID that already contains a "/" (whether "models/*"
// or "tunedModels/*") is used verbatim, and one without a "/" is prefixed
// with "models/".
func TestGetModelPath(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    string
	}{
		{"passes through models/* path", "models/some-model", "models/some-model"},
		{"passes through tunedModels/* path", "tunedModels/some-model", "tunedModels/some-model"},
		{"adds models/ prefix when no slash", "some-model", "models/some-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetModelPath(tt.modelID); got != tt.want {
				t.Fatalf("GetModelPath(%q) = %q, want %q", tt.modelID, got, tt.want)
			}
		})
	}
}
