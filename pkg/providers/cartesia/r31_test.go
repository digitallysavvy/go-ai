package cartesia

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS JSON.stringify(requestBody) keeps object-literal order and leaves HTML
// characters literal.
func TestSpeechRequestBodyKeyOrder(t *testing.T) {
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{1})
	})
	model, _ := p.SpeechModel(ModelSonic35)
	speed := 1.2
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "a<b & c", Voice: "v", Language: "en", Speed: &speed})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model_id":"sonic-3.5","transcript":"a<b & c","voice":{"mode":"id","id":"v"},"output_format":{"container":"mp3","sample_rate":44100,"bit_rate":128000},"language":"en","generation_config":{"speed":1.2}}`
	if res.Request.Body != want {
		t.Fatalf("request.body = %s\nwant %s", res.Request.Body, want)
	}
}

// TS `timestampGranularities != null` is true for an empty array.
func TestStreamEmptyTimestampGranularitiesStillWarns(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()
	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		ProviderOptions: map[string]interface{}{
			"cartesia": map[string]interface{}{"timestampGranularities": []interface{}{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	first, err := result.Stream.Next()
	if err != nil || len(first.Warnings) != 1 || first.Warnings[0].Feature != "providerOptions.cartesia.timestampGranularities" {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
}
