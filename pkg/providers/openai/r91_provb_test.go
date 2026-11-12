package openai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
