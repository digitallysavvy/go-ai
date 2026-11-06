package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r55CollectBedrockStream(t *testing.T, events [][2]string) ([]*provider.StreamChunk, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody(events))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			return chunks, nil
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, c)
	}
}

// TS safeParseJSON accepts any JSON value; a non-object payload for a known
// event type is wrapped as {[payloadType]: value} (the raw chunk) and then
// fails AmazonBedrockStreamSchema validation as an error part. The stream
// keeps going, and finishReason stays error unless a later messageStop sets it.
func TestR55BedrockStreamNonObjectPayloadForKnownTypeIsValidationError(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want interface{}
	}{
		{"number", `5`, float64(5)},
		{"string", `"x"`, "x"},
		{"array", `[]`, []interface{}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := r55CollectBedrockStream(t, [][2]string{{"contentBlockDelta", tc.data}})
			if err != nil {
				t.Fatalf("stream error = %v, want none", err)
			}
			var raw, errChunk, finish *provider.StreamChunk
			for _, c := range chunks {
				switch c.Type {
				case provider.ChunkTypeRaw:
					raw = c
				case provider.ChunkTypeError:
					errChunk = c
				case provider.ChunkTypeFinish:
					finish = c
				}
			}
			wantRaw := map[string]interface{}{"contentBlockDelta": tc.want}
			if raw == nil || !reflect.DeepEqual(raw.Raw, wantRaw) {
				t.Fatalf("raw chunk = %#v, want %#v", raw, wantRaw)
			}
			if errChunk == nil {
				t.Fatal("no error part")
			}
			if providererrors.IsJSONParseError(errChunk.Err) {
				t.Fatalf("error part is a JSON parse error: %v", errChunk.Err)
			}
			if finish == nil || finish.FinishReason != types.FinishReasonError {
				t.Fatalf("finish = %#v, want finishReason error", finish)
			}
		})
	}
}

// For an unknown payload type the schema strips the key, so a non-object
// payload passes validation: TS emits only the raw chunk.
func TestR55BedrockStreamNonObjectPayloadForUnknownTypeIsRawOnly(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{
		{"someFutureEvent", `5`},
		{"messageStop", `{"stopReason":"end_turn"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raws []interface{}
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeRaw {
			raws = append(raws, c.Raw)
		}
		if c.Type == provider.ChunkTypeFinish && c.FinishReason != types.FinishReasonStop {
			t.Fatalf("finishReason = %q, want stop", c.FinishReason)
		}
	}
	if len(raws) == 0 || !reflect.DeepEqual(raws[0], map[string]interface{}{"someFutureEvent": float64(5)}) {
		t.Fatalf("raw chunks = %#v", raws)
	}
}

// A JSON null payload makes TS throw on `delete null.p`, which errors the
// stream itself: no raw chunk, no error part and no finish part follow.
func TestR55BedrockStreamNullPayloadErrorsStream(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{
		{"contentBlockDelta", `null`},
		{"messageStop", `{"stopReason":"end_turn"}`},
	})
	if err == nil || err.Error() != "Cannot convert undefined or null to object" {
		t.Fatalf("stream error = %v, want the TS TypeError text", err)
	}
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeRaw || c.Type == provider.ChunkTypeError || c.Type == provider.ChunkTypeFinish {
			t.Fatalf("unexpected %s chunk before the stream error", c.Type)
		}
	}
}
