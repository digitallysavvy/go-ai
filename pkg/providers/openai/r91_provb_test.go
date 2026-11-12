package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PB-01 (provb1-11): the decision, Live, transcription stream, files and
// batch option readers read Go-typed literals like their JSON form.
func TestR91provbTypedOptionReaders(t *testing.T) {
	sid := "user-1"
	t.Run("decision options", func(t *testing.T) {
		got, err := parseDecisionProviderOptions(map[string]interface{}{"openai": map[string]string{"safetyIdentifier": sid}})
		if err != nil || got.SafetyIdentifier == nil || *got.SafetyIdentifier != sid {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, err = parseDecisionProviderOptions(map[string]interface{}{"openai": &DecisionModelOptions{SafetyIdentifier: &sid}})
		if err != nil || got.SafetyIdentifier == nil || *got.SafetyIdentifier != sid {
			t.Fatalf("typed struct: got %+v, %v", got, err)
		}
	})
	t.Run("decision image part detail", func(t *testing.T) {
		img, err := decisionImageContent(provider.DecisionStatePart{Type: "file", File: &types.FileContent{
			MediaType: "image/png", Data: []byte{1}, ProviderOptions: map[string]interface{}{"openai": map[string]string{"imageDetail": "high"}},
		}})
		if err != nil || img["detail"] != "high" {
			t.Fatalf("got %v, %v", img, err)
		}
	})
	t.Run("live input and voice", func(t *testing.T) {
		typed := map[string]interface{}{"openai": map[string]interface{}{
			"input": []map[string]interface{}{{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": "hi"}}}},
			"voice": map[string]string{"id": "v1"},
		}}
		generic := map[string]interface{}{"openai": map[string]interface{}{
			"input": []interface{}{map[string]interface{}{"type": "message", "role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "hi"}}}},
			"voice": map[string]interface{}{"id": "v1"},
		}}
		want, err := parseOpenAILiveOptions(generic)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseOpenAILiveOptions(typed)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, %v; want %+v", got, err, want)
		}
	})
	t.Run("transcription stream", func(t *testing.T) {
		got := TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]string{"language": "en"}})
		if !got.HasLanguage || got.Language != "en" {
			t.Fatalf("got %+v", got)
		}
		got = TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]interface{}{"streaming": map[string]int64{"delay": 300}}})
		if got.StreamingDelay == nil || *got.StreamingDelay != 300 {
			t.Fatalf("delay: got %+v", got)
		}
	})
	t.Run("files options", func(t *testing.T) {
		got, err := parseOpenAIFilesOptions(map[string]interface{}{"openai": map[string]string{"purpose": "batch"}})
		if err != nil || got.purpose != "batch" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("batch options", func(t *testing.T) {
		got, err := openaiBatchInputFileExpiresAfter("openai", map[string]interface{}{"openai": map[string]int64{"inputFileExpiresAfter": 7200}})
		if err != nil || got != 7200 {
			t.Fatalf("got %d, %v", got, err)
		}
	})
	t.Run("speech speed kinds", func(t *testing.T) {
		for _, v := range []interface{}{int8(2), int32(2), int64(2), uint(2), float32(2)} {
			if err := ValidateSpeechProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"speed": v}}); err != nil {
				t.Fatalf("%T: %v", v, err)
			}
			if f, ok := speechSpeedOption(v); !ok || f != 2 {
				t.Fatalf("%T: %v %v", v, f, ok)
			}
		}
	})
}

// PB-03 (provc-04): an OpenAI-compatible wrapper has no input byte budget
// (TS OpenAICompatibleEmbeddingModel has none); native OpenAI keeps 300000.
func TestR91provbCompatEmbeddingByteBudget(t *testing.T) {
	native, _ := New(Config{APIKey: "k"}).EmbeddingModel("text-embedding-3-small")
	compat, _ := New(Config{APIKey: "k", Name: "acme", OpenAICompatible: true}).EmbeddingModel("e")
	if got := native.(*EmbeddingModel).MaxInputBytesPerCall(); got != 300_000 {
		t.Fatalf("native = %d", got)
	}
	if got := compat.(*EmbeddingModel).MaxInputBytesPerCall(); got != 0 {
		t.Fatalf("compat = %d, want 0", got)
	}
}

// core2-18 (openai part): a decision file part with string data is sent
// verbatim like TS (openai-decision-model.ts:192); unpadded base64 failed the
// strict decode.
func TestR91provbDecisionFileDataStringVerbatim(t *testing.T) {
	img, err := decisionImageContent(provider.DecisionStatePart{Type: "file", File: &types.FileContent{
		MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw0KGgo"},
	}})
	if err != nil || img["image_url"] != "data:image/png;base64,iVBORw0KGgo" {
		t.Fatalf("got %v, %v", img, err)
	}
}

// OVC-02 (openai model_catalog.go site): a transport failure is the
// retryable "Cannot connect to API" error (TS handleFetchError).
func TestR91provbModelCatalogTransportError(t *testing.T) {
	_, err := ModelCatalogRefresher{APIKey: "k", Endpoint: "http://127.0.0.1:1/v1/models"}.Fetch(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") {
		t.Fatalf("err = %v", err)
	}
}

func r91provbDrainStream(s *responsesStream) ([]*provider.StreamChunk, error) {
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, c)
	}
}

func r91provbFinish(chunks []*provider.StreamChunk) *provider.StreamChunk {
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			return c
		}
	}
	return nil
}

// PB-08 (provb1-01): a fractional usage count is read truncated (TS
// z.number()), and an undecodable response.failed is an error part followed
// by the finish part (TS flush), not a silent io.EOF.
func TestR91provbResponsesStreamFractionalUsage(t *testing.T) {
	sse := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\"}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"m1\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"m1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1.5,\"output_tokens\":2,\"total_tokens\":3.5}}}\n\n"
	chunks, err := r91provbDrainStream(newResponsesStream(io.NopCloser(strings.NewReader(sse)), false))
	if err != nil {
		t.Fatal(err)
	}
	f := r91provbFinish(chunks)
	if f == nil || f.FinishReason != types.FinishReasonStop || f.Usage == nil || f.Usage.InputTokens == nil || *f.Usage.InputTokens != 1 {
		t.Fatalf("finish = %+v", f)
	}
}

// PB-19 (provb1-13): the mid-stream response.failed error part's data is
// TS's synthetic {type, sequence_number, response: {error,
// incomplete_details, service_tier}} frame, not the whole event.
func TestR91provbResponsesFailedErrorData(t *testing.T) {
	sse := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\"}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"m1\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"m1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"sequence_number\":5,\"response\":{\"id\":\"r\",\"error\":{\"code\":\"server_error\",\"message\":\"x\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1},\"output\":[]}}\n\n"
	chunks, err := r91provbDrainStream(newResponsesStream(io.NopCloser(strings.NewReader(sse)), false))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("err = %#v", c.Err)
		}
		b, _ := json.Marshal(spe.Data)
		want := `{"response":{"error":{"code":"server_error","message":"x"}},"sequence_number":5,"type":"response.failed"}`
		var got interface{}
		_ = json.Unmarshal(b, &got)
		g, _ := json.Marshal(got)
		if string(g) != want {
			t.Fatalf("data = %s, want %s", g, want)
		}
		return
	}
	t.Fatal("no error part")
}

// PB-10 (provb1-03): after response.in_progress, a read that blocks past
// TS's 50ms acceptedGraceMs hands the stream out, so a later error frame is
// an error part followed by finish(error), not a failed Next.
func TestR91provbResponsesAcceptedGrace(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\"}}\n\n" +
			"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\"}}\n\n"))
		time.Sleep(120 * time.Millisecond)
		_, _ = pw.Write([]byte("data: {\"type\":\"error\",\"sequence_number\":3,\"code\":\"server_error\",\"message\":\"boom\",\"param\":null}\n\n"))
		_ = pw.Close()
	}()
	chunks, err := r91provbDrainStream(newResponsesStream(pr, false))
	if err != nil {
		t.Fatalf("Next error = %v, want the stream handed out", err)
	}
	var kinds []provider.ChunkType
	for _, c := range chunks {
		kinds = append(kinds, c.Type)
	}
	f := r91provbFinish(chunks)
	if len(chunks) < 3 || chunks[0].Type != provider.ChunkTypeResponseMetadata || f == nil || f.FinishReason != types.FinishReasonError {
		t.Fatalf("chunks = %v", kinds)
	}
}
