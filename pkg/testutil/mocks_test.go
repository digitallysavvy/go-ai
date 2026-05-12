package testutil

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMockLanguageModelDefaultsAndTracking(t *testing.T) {
	m := &MockLanguageModel{ToolSupport: true}
	if m.Provider() != "mock" || m.ModelID() != "mock-model" || !m.SupportsTools() {
		t.Fatalf("unexpected language model metadata")
	}

	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil || res.Text == "" {
		t.Fatalf("DoGenerate result=%#v err=%v", res, err)
	}
	if len(m.GenerateCalls) != 1 {
		t.Fatalf("generate calls = %d", len(m.GenerateCalls))
	}

	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	var chunks int
	for {
		_, e := stream.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("stream.Next: %v", e)
		}
		chunks++
	}
	if chunks == 0 || len(m.StreamCalls) != 1 {
		t.Fatalf("expected stream chunks and one tracked call")
	}
}

func TestMockEmbeddingAndRerankModels(t *testing.T) {
	em := &MockEmbeddingModel{MaxBatchSize: 7, ParallelSupport: true}
	if em.MaxEmbeddingsPerCall() != 7 || !em.SupportsParallelCalls() {
		t.Fatalf("embedding capabilities mismatch")
	}
	one, err := em.DoEmbed(context.Background(), "hello", nil)
	if err != nil || len(one.Embedding) == 0 {
		t.Fatalf("DoEmbed result=%#v err=%v", one, err)
	}
	many, err := em.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil || len(many.Embeddings) != 2 {
		t.Fatalf("DoEmbedMany result=%#v err=%v", many, err)
	}
	if len(em.EmbedCalls) != 1 || len(em.EmbedManyCalls) != 1 {
		t.Fatalf("embedding call tracking mismatch")
	}

	rm := &MockRerankingModel{}
	rr, err := rm.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "q",
		Documents: []string{"d1", "d2", "d3"},
	})
	if err != nil || len(rr.Ranking) != 3 {
		t.Fatalf("DoRerank result=%#v err=%v", rr, err)
	}
	if rr.Response.ModelID != "mock-reranking" {
		t.Fatalf("model id mismatch: %q", rr.Response.ModelID)
	}
}

func TestMockTextStreamHelpers(t *testing.T) {
	s := NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	buf := make([]byte, 16)
	n, err := s.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("Read result n=%d err=%v", n, err)
	}
	_ = s.Close()
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after Close should return EOF, got %v", err)
	}

	streamErr := errors.New("boom")
	se := NewMockTextStreamWithError(streamErr)
	if _, err := se.Next(); !errors.Is(err, streamErr) {
		t.Fatalf("expected configured stream error, got %v", err)
	}
}
