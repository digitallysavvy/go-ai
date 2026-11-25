package anthropic

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// Ruling 4 (r96): a 1e400 in the loose usage object reads as +Inf
// (Usage.Raw, providerMetadata.anthropic.usage) and is written as null when
// serialized.
func TestR96provaUsageOutOfRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",
			"content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":4,"output_tokens":3,"x_extra":1e400}}`))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	anth, _ := result.ProviderMetadata["anthropic"].(map[string]interface{})
	usage, _ := anth["usage"].(map[string]interface{})
	// TS providerMetadata.anthropic.usage is response.usage itself, so it
	// holds Infinity in memory; JSON.stringify writes it as null.
	if v, _ := usage["x_extra"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("providerMetadata.anthropic.usage = %#v, want x_extra +Inf", anth["usage"])
	}
	if b, err := jsonstr.Marshal(anth["usage"]); err != nil || !strings.Contains(string(b), `"x_extra":null`) {
		t.Fatalf("serialized usage = %s, %v, want x_extra null", b, err)
	}
}

// Ruling 4 (r96): citation metadata with a non-finite number is written with
// null (JSON.stringify), not dropped.
func TestR96provaCitationMetadataNonFinite(t *testing.T) {
	src, ok := createCitationSource(map[string]interface{}{
		"type": "page_location", "cited_text": "c", "document_index": float64(0),
		"start_page_number": math.Inf(1), "end_page_number": float64(2),
	}, []citationDocument{{Title: "d", MediaType: "application/pdf"}}, func() string { return "id" })
	if !ok || !strings.Contains(string(src.ProviderMetadata), `"startPageNumber":null`) {
		t.Fatalf("source = %+v (metadata %s)", src, src.ProviderMetadata)
	}
	meta := citationsProviderMetadata([]map[string]interface{}{{"type": "page_location", "start_page_number": math.NaN()}})
	if !strings.Contains(string(meta), `"start_page_number":null`) {
		t.Fatalf("citations metadata = %s", meta)
	}
}
