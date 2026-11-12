package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getHeaders/loadApiKey throws LoadAPIKeyError, which postJsonToApi does
// not wrap: the Interactions model returns it as is (not a retryable
// ProviderError).
func TestR92provgInteractionsMissingKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", "")
	sent := 0
	hc := &http.Client{Transport: missingKeyRT(func(r *http.Request) (*http.Response, error) {
		sent++
		return nil, errors.New("stop")
	})}
	p := New(Config{HTTPClient: hc})
	m, err := p.Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	_, err = m.DoGenerate(t.Context(), opts)
	if _, direct := err.(*providererrors.LoadAPIKeyError); !direct || sent != 0 {
		t.Fatalf("DoGenerate err = %T %v sent = %d", err, err, sent)
	}
	_, err = m.DoStream(t.Context(), opts)
	if _, direct := err.(*providererrors.LoadAPIKeyError); !direct || sent != 0 {
		t.Fatalf("DoStream err = %T %v sent = %d", err, err, sent)
	}
}

// TS extract-google-interactions-sources.ts basename() is undefined for a
// trailing "/": the document title falls back to the uri (file_name ??
// filename ?? uri), and no filename is set.
func TestR92provgInteractionsDocumentSourceTrailingSlashTitle(t *testing.T) {
	gen := func() string { return "id" }
	ann := annotationsToSources([]interactionsAnnotation{{Type: "file_citation", DocumentURI: "gs://bucket/dir/"}}, gen)
	if len(ann) != 1 {
		t.Fatalf("annotation sources = %#v", ann)
	}
	if src := ann[0].(types.SourceContent); src.Title != "gs://bucket/dir/" || src.Filename != "" || src.SourceType != "document" {
		t.Fatalf("annotation source = %#v", src)
	}
	block := interactionsContentBlock{Type: "file_search_result", Result: []interface{}{
		map[string]interface{}{"document_uri": "gs://bucket/dir/"},
		map[string]interface{}{"document_uri": "gs://bucket/dir/", "file_name": ""},
		map[string]interface{}{"document_uri": "gs://bucket/dir/", "title": "T"},
	}}
	res := builtinToolResultSources(block, gen)
	want := []string{"gs://bucket/dir/", "", "T"}
	if len(res) != len(want) {
		t.Fatalf("tool result sources = %#v", res)
	}
	for i, w := range want {
		if src := res[i].(types.SourceContent); src.Title != w || src.Filename != "" {
			t.Fatalf("source %d = %#v, want title %q", i, src, w)
		}
	}
}

// TS googleDecisionResponseSchema parses usage.input_tokens_by_modality as
// z.array(z.object({modality, tokens})): undeclared keys of an entry are
// stripped from providerMetadata.google.usage.
func TestR92provgDecisionUsageModalityEntriesStripped(t *testing.T) {
	body := strings.Replace(r91provgDecisionFixture, `"modality":"text"`, `"modality":"text","extra":1`, 1)
	if body == r91provgDecisionFixture {
		t.Fatal("fixture shape changed")
	}
	m, _, _ := r91provgDecisionSetup(t, body, 200)
	res, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := json.Marshal(res.ProviderMetadata["google"].(map[string]interface{})["usage"])
	if want := `{"input_tokens_by_modality":[{"modality":"text","tokens":193}],"total_input_tokens":193,"total_output_tokens":0,"total_tokens":193}`; string(usage) != want {
		t.Fatalf("usage = %s", usage)
	}
}
