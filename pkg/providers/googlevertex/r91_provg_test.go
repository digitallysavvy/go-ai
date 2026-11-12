package googlevertex

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// provg1-01 (typed-option probe): Vertex option readers accept Go-typed
// literals the way TS zod parses their JSON form.
func TestR91provgTypedOptionLiterals(t *testing.T) {
	emb := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"googleVertex": map[string]string{"taskType": "RETRIEVAL_QUERY", "title": "t"}}}
	if err := validateVertexEmbeddingOptions(emb); err != nil {
		t.Fatalf("embedding validate: %v", err)
	}
	if got := vertexEmbeddingOptions(emb); got.TaskType != "RETRIEVAL_QUERY" || got.Title != "t" {
		t.Fatalf("embedding options = %+v", got)
	}
	gt, err := parseVertexGeminiTranscriptionOptionsChecked(map[string]interface{}{"vertex": map[string][]string{"languageCodes": {"en-US"}}})
	if err != nil || len(gt.LanguageCodes) != 1 {
		t.Fatalf("gemini transcription: %+v, %v", gt, err)
	}
	ct, err := vertexTranscriptionOptionsChecked(map[string]interface{}{"googleVertex": map[string]string{"region": "eu"}})
	if err != nil || ct.Region != "eu" {
		t.Fatalf("cloud transcription: %+v, %v", ct, err)
	}
	if img := extractVertexOptions(map[string]interface{}{"googleVertex": map[string]string{"negativePrompt": "x"}}); img["negativePrompt"] != "x" {
		t.Fatalf("image options = %#v", img)
	}
	vm := &VideoModel{}
	if poll := vm.getPollOptions(map[string]interface{}{"googleVertex": map[string]int64{"pollIntervalMs": 1234}}); poll.PollIntervalMs != 1234 {
		t.Fatalf("video poll = %+v", poll)
	}
}
