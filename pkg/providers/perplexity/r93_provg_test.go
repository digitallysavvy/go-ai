package perplexity

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS new Date(created_at * 1000): a Date holds whole milliseconds, so the
// sub-millisecond part of a fractional created_at is dropped (TimeClip
// truncates toward zero).
func TestR93provgCreatedAtMillisecondPrecision(t *testing.T) {
	got := perplexityCreatedAt(1700000000.1234567)
	if want := time.UnixMilli(1700000000123); !got.Equal(want) {
		t.Fatalf("created = %v (%d ns), want %v", got, got.UnixNano(), want)
	}
	if got := perplexityCreatedAt(-1.0005); !got.Equal(time.UnixMilli(-1000)) {
		t.Fatalf("negative created = %d ms", got.UnixMilli())
	}
}

// TS raw finish reason is incomplete_details?.reason ?? status: a present
// empty reason is kept instead of falling back to the status.
func TestR93provgRawFinishReasonPresentEmptyReason(t *testing.T) {
	body := []byte(`{"id":"r","created_at":1,"model":"m","status":"incomplete","incomplete_details":{"reason":""},"output":[]}`)
	var resp perplexityAgentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{}
	if got := m.convertResponse(&resp, body).RawFinishReason; got != "" {
		t.Fatalf("generate raw finish reason = %q", got)
	}
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":"+string(body)+"}\n\n")), nil, false, nil)
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.RawFinishReason != "" {
		t.Fatalf("stream finish = %+v", finish)
	}
}
