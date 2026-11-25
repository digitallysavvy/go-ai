package huggingface

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r98provbFinish drains an hfStream over one SSE body and returns its finish
// part.
func r98provbFinish(t *testing.T, sse string) *provider.StreamChunk {
	t.Helper()
	s := newHFStream(io.NopCloser(strings.NewReader(sse)), nil, "huggingface.responses")
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("err = %v", err)
			}
			return finish
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
}

// TestR98provbStreamFractionalUsage: TS z.number() accepts a fractional
// token count, so a response.completed frame with one sets the finish reason
// and usage (raw keeps 10.5). Go failed the typed decode, skipped the frame
// and finished as interrupted with no usage.
func TestR98provbStreamFractionalUsage(t *testing.T) {
	sse := `data: {"type":"response.completed","response":{"id":"r","model":"m","object":"response","created_at":1,"status":"completed","error":null,"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto","tools":[],"temperature":1,"top_p":1,"usage":{"input_tokens":10.5,"output_tokens":3,"total_tokens":13.5},"output":[]},"sequence_number":1}` + "\n\n"
	finish := r98provbFinish(t, sse)
	if finish == nil || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish = %+v, want stop", finish)
	}
	if finish.Usage == nil || finish.Usage.Raw["input_tokens"] != 10.5 || finish.Usage.Raw["total_tokens"] != 13.5 {
		t.Fatalf("usage = %+v, want raw input_tokens 10.5", finish.Usage)
	}
}

// TestR98provbStreamNonObjectResponse: a response.completed frame whose
// response is not an object matches TS's loose fallback member; transform
// reads undefined incomplete_details, so the finish reason is stop. Go
// skipped the frame (interrupted).
func TestR98provbStreamNonObjectResponse(t *testing.T) {
	finish := r98provbFinish(t, `data: {"type":"response.completed","response":5}`+"\n\n")
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "" {
		t.Fatalf("finish = %+v, want stop with no raw reason", finish)
	}
}

// TestR98provbGenerateFractionalUsageRaw: TS usage.raw is the parsed usage
// object, so a fractional count stays exact (Go re-encoded the truncated
// int fields), and only the schema's declared keys are kept.
func TestR98provbGenerateFractionalUsageRaw(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"m","object":"response","created_at":1,"status":"completed","error":null,"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto","tools":[],"temperature":1,"top_p":1,"usage":{"input_tokens":12.5,"input_tokens_details":{"cached_tokens":2.25,"extra":1},"output_tokens":25,"total_tokens":37.5,"other":9},"output":[]}`))
	}, "m")
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"input_tokens":         12.5,
		"input_tokens_details": map[string]interface{}{"cached_tokens": 2.25},
		"output_tokens":        float64(25),
		"total_tokens":         37.5,
	}
	if !reflect.DeepEqual(res.Usage.Raw, want) {
		t.Fatalf("raw = %#v, want %#v", res.Usage.Raw, want)
	}
}
