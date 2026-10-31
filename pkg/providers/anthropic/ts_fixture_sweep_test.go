package anthropic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Every recorded TS fixture (packages/anthropic/src/__fixtures__) must run
// through the Go provider without error and produce a finish. Fixtures that
// TS tests assert in detail are covered by named tests; this sweep guarantees
// none is left undriven.
func TestEveryTSFixtureRunsThroughGo(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".chunks.txt"):
			base := strings.TrimSuffix(name, ".chunks.txt")
			t.Run("stream/"+base, func(t *testing.T) {
				model, closeFn := fixtureModel(t, serveChunksFixture(base, t))
				defer closeFn()
				s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				if err != nil {
					t.Fatal(err)
				}
				finishes := 0
				for _, c := range drain(t, s) {
					if c.Type == provider.ChunkTypeFinish {
						finishes++
					}
					// spliced-message-start is meant to end in an error chunk.
					if c.Type == provider.ChunkTypeError && base != "spliced-message-start" {
						t.Fatalf("error chunk: %v", c.Err)
					}
				}
				if finishes == 0 && base != "spliced-message-start" {
					t.Fatal("no finish chunk")
				}
			})
		case strings.HasSuffix(name, ".json") && name != "decision.json" && name != "snapshots.json":
			base := strings.TrimSuffix(name, ".json")
			t.Run("generate/"+base, func(t *testing.T) {
				model, closeFn := fixtureModel(t, serveJSONFixture(base, t))
				defer closeFn()
				res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				if err != nil {
					t.Fatal(err)
				}
				if res.FinishReason == "" {
					t.Fatalf("finish=%q", res.FinishReason)
				}
			})
		}
	}
}
