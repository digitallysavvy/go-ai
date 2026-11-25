package ai

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays packages/ai/src/util/cosine-similarity.test.ts and
// generate-text/sum-token-counts.test.ts case for case.

func TestTSReplay_CosineSimilarity(t *testing.T) {
	close5 := func(got, want float64) bool { return math.Abs(got-want) < 0.5e-5 }

	if got, err := CosineSimilarity([]float64{1, 2, 3}, []float64{4, 5, 6}); err != nil || !close5(got, 0.9746318461970762) {
		t.Fatalf("positive: %v, %v", got, err)
	}
	if got, err := CosineSimilarity([]float64{1, 0}, []float64{-1, 0}); err != nil || !close5(got, -1) {
		t.Fatalf("negative: %v, %v", got, err)
	}
	_, err := CosineSimilarity([]float64{1, 2, 3}, []float64{4, 5})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("different lengths: error = %v, want InvalidArgumentError", err)
	}
	zero := []float64{0, 0, 0}
	v := []float64{0, 1, 2}
	if got, _ := CosineSimilarity(v, zero); got != 0 {
		t.Fatalf("zero vector (second) = %v", got)
	}
	if got, _ := CosineSimilarity(zero, v); got != 0 {
		t.Fatalf("zero vector (first) = %v", got)
	}
	if got, _ := CosineSimilarity([]float64{1e-10, 0, 0}, []float64{2e-10, 0, 0}); got != 1 {
		t.Fatalf("small magnitudes = %v, want exactly 1", got)
	}
	if got, _ := CosineSimilarity([]float64{1e-10, 0, 0}, []float64{-1e-10, 0, 0}); got != -1 {
		t.Fatalf("small opposite magnitudes = %v, want exactly -1", got)
	}
}

func TestTSReplay_SumTokenCounts(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	if got := sumTokenCounts(n(3), n(10)); got == nil || *got != 13 {
		t.Fatalf("3+10 = %v", got)
	}
	if got := sumTokenCounts(nil, n(10)); got == nil || *got != 10 {
		t.Fatalf("undefined+10 = %v", got)
	}
	if got := sumTokenCounts(n(3), nil); got == nil || *got != 3 {
		t.Fatalf("3+undefined = %v", got)
	}
	if got := sumTokenCounts(nil, nil); got != nil {
		t.Fatalf("undefined+undefined = %v, want nil", *got)
	}
}

// Replays text-stream/to-text-stream.test.ts and
// generate-text/filter-active-tools.test.ts.

func TestTSReplay_ToTextStreamKeepsOnlyTextDeltas(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStart},
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: ", world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
	})
	out, errs := ToTextStream(context.Background(), stream)
	var got []string
	for s := range out {
		got = append(got, s)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"Hello", ", world!"}) {
		t.Fatalf("text = %q", got)
	}
}

func TestTSReplay_FilterActiveTools(t *testing.T) {
	tool1 := types.Tool{Name: "tool1", Description: "Tool 1 description"}
	tool2 := types.Tool{Name: "tool2", Description: "Tool 2 description"}
	providerTool := types.Tool{Name: "providerTool", Type: "provider"}
	all := []types.Tool{tool1, tool2, providerTool}

	if got := FilterActiveTools(nil, []string{"tool1"}); got != nil {
		t.Fatalf("no tools: %v", got)
	}
	if got := FilterActiveTools(all, nil); !reflect.DeepEqual(got, all) {
		t.Fatalf("activeTools not provided should return all tools, got %v", got)
	}
	if got := FilterActiveTools(all, []string{}); len(got) != 0 {
		t.Fatalf("empty activeTools should return no tools, got %v", got)
	}
	got := FilterActiveTools(all, []string{"tool1", "providerTool"})
	if len(got) != 2 || got[0].Name != "tool1" || got[1].Name != "providerTool" {
		t.Fatalf("filtered = %v", got)
	}
}
