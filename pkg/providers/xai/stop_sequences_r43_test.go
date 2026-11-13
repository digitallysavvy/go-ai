package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS warns when stopSequences != null, so an empty array warns as well.
func TestXAIResponsesWarnings_EmptyStopSequencesWarn(t *testing.T) {
	warnings := xaiResponsesWarnings(&provider.GenerateOptions{StopSequences: []string{}})
	if len(warnings) != 1 || warnings[0].Feature != "stopSequences" {
		t.Fatalf("warnings = %#v", warnings)
	}
}
