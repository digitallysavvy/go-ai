package ai

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r69EmptyStream struct{}

func (r69EmptyStream) Next() (*provider.StreamChunk, error) { return nil, io.EOF }
func (r69EmptyStream) Err() error                           { return nil }
func (r69EmptyStream) Close() error                         { return nil }

// TS streamText ends a run whose model call fails to open, or whose model
// stream ends without output, with one `error` part and a normally closing
// stream, so toUIMessageStream writes that part's error chunk only.
func TestR69UIStreamErrorPartEndWritesOneErrorChunk(t *testing.T) {
	openErr := errors.New("open failed")
	cases := map[string]struct {
		doStream func() (provider.TextStream, error)
		wantErr  func(error) bool
	}{
		"open failure": {
			doStream: func() (provider.TextStream, error) { return nil, openErr },
			wantErr:  func(err error) bool { return errors.Is(err, openErr) },
		},
		"no output": {
			doStream: func() (provider.TextStream, error) { return r69EmptyStream{}, nil },
			wantErr:  IsNoOutputGeneratedError,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return tc.doStream()
			}}
			result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			var outcome map[string]interface{}
			chunks, errCh := CreateUIMessageStream(context.Background(), result, UIMessageStreamResultOptions{
				OnEnd: func(e map[string]interface{}) { outcome, _ = e["outcome"].(map[string]interface{}) },
			})
			var types []string
			for c := range chunks {
				types = append(types, c["type"].(string))
			}
			if got, want := len(types), 2; got != want || types[0] != "start" || types[1] != "error" {
				t.Fatalf("chunk types = %v, want [start error]", types)
			}
			if streamErr := <-errCh; !tc.wantErr(streamErr) {
				t.Fatalf("stream error = %v", streamErr)
			}
			if outcome["status"] != "failed" {
				t.Fatalf("outcome = %v, want failed", outcome)
			}
			if e, _ := outcome["error"].(error); !tc.wantErr(e) {
				t.Fatalf("outcome error = %v", outcome["error"])
			}
		})
	}
}
