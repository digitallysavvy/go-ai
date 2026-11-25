package google

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS emits a reasoning-delta for every text summary item whose text is a
// string, empty included, both on step.start and on thought_summary deltas.
func TestR55InteractionsStreamEmptyThoughtSummaryTextIsReasoningDelta(t *testing.T) {
	body := strings.Join([]string{
		`data: {"event_type":"step.start","index":0,"step":{"type":"thought","summary":[{"type":"text","text":""},{"type":"text"},{"type":"text","text":"a"}]}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"thought_summary","content":{"type":"text","text":""}}}`,
		``,
		`data: {"event_type":"step.stop","index":0}`,
		``,
		``,
	}, "\n")
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	var got []string
	for _, c := range drainInteractions(t, s) {
		if c.Type == provider.ChunkTypeReasoning {
			got = append(got, c.Reasoning)
		}
	}
	if strings.Join(got, "|") != "|a|" || len(got) != 3 {
		t.Fatalf("reasoning deltas = %q, want [\"\" \"a\" \"\"]", got)
	}
}
