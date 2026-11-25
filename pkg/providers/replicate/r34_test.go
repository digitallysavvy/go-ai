package replicate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A non-2xx poll response is an API error, not an in-progress prediction.
func TestR34_LanguagePollHTTPErrorIsReported(t *testing.T) {
	polls := 0
	var pollHeader string
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return replicateJSONResponse(200, `{"id":"p1","status":"starting"}`), nil
		}
		polls++
		pollHeader = r.Header.Get("X-Call")
		return replicateJSONResponse(401, `{"detail":"bad token"}`), nil
	})
	_, err := NewLanguageModel(p, "v").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hi"},
		Headers: map[string]string{"X-Call": "1"},
	})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %T %v, want ProviderError", err, err)
	}
	if polls != 1 {
		t.Errorf("polls = %d, want 1 (no silent retry loop)", polls)
	}
	if pollHeader != "1" {
		t.Errorf("poll request dropped call headers: %q", pollHeader)
	}
}

// The simulated stream never splits a multi-byte rune and reports io.EOF once
// finished or closed (TextStream contract).
func TestR34_LanguageStreamRuneSafeAndEOF(t *testing.T) {
	text := strings.Repeat("héllo wörld ✓ ", 8)
	s := &replicateStream{result: &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}}
	var got strings.Builder
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatalf("Next before finish: %v", err)
		}
		if c.Type == provider.ChunkTypeFinish {
			break
		}
		if !utf8.ValidString(c.Text) {
			t.Fatalf("chunk %q is not valid UTF-8", c.Text)
		}
		got.WriteString(c.Text)
	}
	if got.String() != text {
		t.Errorf("reassembled text differs")
	}
	if _, err := s.Next(); err != io.EOF {
		t.Errorf("Next after finish = %v, want io.EOF", err)
	}
	s2 := &replicateStream{result: &types.GenerateResult{Text: "x"}}
	_ = s2.Close()
	if _, err := s2.Next(); err != io.EOF {
		t.Errorf("Next after Close = %v, want io.EOF", err)
	}
}
