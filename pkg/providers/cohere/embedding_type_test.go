package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func embedTestModel(t *testing.T, respBody string, seen *map[string]interface{}, calls *int) *EmbeddingModel {
	t.Helper()
	p := newCohereProviderWithTransportForEmbed(t, func(r *http.Request) (*http.Response, error) {
		*calls++
		_ = json.NewDecoder(r.Body).Decode(seen)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(respBody))}, nil
	})
	return NewEmbeddingModel(p, "embed-english-v3.0")
}

func TestCohereEmbeddingTypeSelection(t *testing.T) {
	for _, et := range []string{"float", "int8", "uint8", "binary", "ubinary"} {
		t.Run(et, func(t *testing.T) {
			var seen map[string]interface{}
			calls := 0
			body := `{"embeddings":{"` + et + `":[[1,-2],[3,0]],"base64":["extra"]},"meta":{"billed_units":{"input_tokens":10}}}`
			m := embedTestModel(t, body, &seen, &calls)
			out, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{
				ProviderOptions: map[string]interface{}{"cohere": map[string]interface{}{"embeddingType": et}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(seen["embedding_types"], []interface{}{et}) {
				t.Fatalf("embedding_types = %#v", seen["embedding_types"])
			}
			if !reflect.DeepEqual(out.Embeddings, [][]float64{{1, -2}, {3, 0}}) {
				t.Fatalf("embeddings = %#v", out.Embeddings)
			}
		})
	}
}

func TestCohereEmbeddingTypeDefaultsToFloatAndConstructorOption(t *testing.T) {
	var seen map[string]interface{}
	calls := 0
	m := embedTestModel(t, `{"embeddings":{"float":[[0.5]],"int8":[[9]]},"meta":{"billed_units":{"input_tokens":1}}}`, &seen, &calls)
	out, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil || out.Embeddings[0][0] != 0.5 {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if !reflect.DeepEqual(seen["embedding_types"], []interface{}{"float"}) {
		t.Fatalf("embedding_types = %#v", seen["embedding_types"])
	}
	m.options.EmbeddingType = EmbeddingTypeInt8
	out, err = m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil || out.Embeddings[0][0] != 9 {
		t.Fatalf("out=%#v err=%v", out, err)
	}
}

func TestCohereEmbeddingTypeErrors(t *testing.T) {
	var seen map[string]interface{}
	calls := 0
	m := embedTestModel(t, `{"embeddings":{"float":[[0.1]]},"meta":{"billed_units":{"input_tokens":1}}}`, &seen, &calls)
	for _, bad := range []interface{}{"base64", "invalid", "", []string{"float", "int8"}, nil} {
		_, err := m.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"cohere": map[string]interface{}{"embeddingType": bad}},
		})
		if err == nil || !strings.Contains(err.Error(), "invalid cohere provider options") {
			t.Fatalf("embeddingType %#v: err = %v", bad, err)
		}
	}
	if calls != 0 {
		t.Fatalf("expected no requests, got %d", calls)
	}
	// Missing requested format.
	_, err := m.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"cohere": map[string]interface{}{"embeddingType": "int8"}},
	})
	if err == nil {
		t.Fatal("expected error for missing int8 embeddings")
	}
	// Non-numeric.
	m2 := embedTestModel(t, `{"embeddings":{"int8":[["x"]]},"meta":{"billed_units":{"input_tokens":1}}}`, &seen, &calls)
	m2.options.EmbeddingType = EmbeddingTypeInt8
	if _, err := m2.DoEmbedMany(context.Background(), []string{"a"}, nil); err == nil {
		t.Fatal("expected error for non-numeric embeddings")
	}
}

func TestCohereEmbed5OutputDimensions(t *testing.T) {
	for _, d := range []OutputDimension{256, 512, 768, 1024, 1536, 2048} {
		var seen map[string]interface{}
		calls := 0
		m := embedTestModel(t, `{"embeddings":{"float":[[1]]},"meta":{"billed_units":{"input_tokens":1}}}`, &seen, &calls)
		m.modelID = "embed-v5.0-pro"
		dd := d
		m.options.OutputDimension = &dd
		if _, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil); err != nil {
			t.Fatalf("%d: %v", d, err)
		}
		if int(seen["output_dimension"].(float64)) != int(d) {
			t.Fatalf("output_dimension = %v", seen["output_dimension"])
		}
	}
}

func TestConvertCohereUsageCachedTokens(t *testing.T) {
	u := convertCohereUsage(json.RawMessage(`{"tokens":{"input_tokens":100,"output_tokens":5},"cached_tokens":40}`))
	var _ types.Usage = u
	if *u.InputDetails.CacheReadTokens != 40 || *u.InputDetails.NoCacheTokens != 60 || *u.InputTokens != 100 {
		t.Fatalf("usage = %#v", u.InputDetails)
	}
	u = convertCohereUsage(json.RawMessage(`{"tokens":{"input_tokens":100,"output_tokens":5}}`))
	if u.InputDetails.CacheReadTokens != nil || *u.InputDetails.NoCacheTokens != 100 {
		t.Fatalf("usage = %#v", u.InputDetails)
	}
}
