package alibaba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestAlibabaEmbeddingModelRequestOptionsAndOrdering(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedUserAgent string
	var capturedProviderHeader string
	var capturedRequestHeader string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedUserAgent = r.Header.Get("User-Agent")
		capturedProviderHeader = r.Header.Get("X-Provider")
		capturedRequestHeader = r.Header.Get("X-Request")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "emb-1")
		_, _ = w.Write([]byte(`{
			"output":{
				"embeddings":[
					{"embedding":[0.4,0.5,0.6],"text_index":1,"sparse_embedding":[{"index":101,"value":0.8,"token":"token-1","ignored":"stripped"}]},
					{"embedding":[0.1,0.2,0.3],"text_index":0,"sparse_embedding":[{"index":100,"value":0.8,"token":"token-0","ignored":"stripped"}]}
				]
			},
			"usage":{"total_tokens":8}
		}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:           "test-api-key",
		EmbeddingBaseURL: server.URL,
		Headers:          map[string]string{"X-Provider": "provider"},
	})
	model, err := p.EmbeddingModel(AlibabaEmbeddingTextV4)
	if err != nil {
		t.Fatalf("EmbeddingModel: %v", err)
	}

	result, err := model.DoEmbedMany(context.Background(), []string{"sunny", "rainy"}, &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Request": "request"},
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"textType":   "query",
				"dimension":  float64(768),
				"outputType": AlibabaEmbeddingOutputDenseSparse,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoEmbedMany: %v", err)
	}

	if capturedPath != "/services/embeddings/text-embedding/text-embedding" {
		t.Fatalf("path = %q", capturedPath)
	}
	if capturedAuth != "Bearer test-api-key" || capturedProviderHeader != "provider" || capturedRequestHeader != "request" {
		t.Fatalf("headers auth=%q provider=%q request=%q", capturedAuth, capturedProviderHeader, capturedRequestHeader)
	}
	if capturedUserAgent != "go-ai/alibaba/0.5.0" {
		t.Fatalf("User-Agent = %q", capturedUserAgent)
	}
	wantBody := map[string]interface{}{
		"model": AlibabaEmbeddingTextV4,
		"input": map[string]interface{}{"texts": []interface{}{"sunny", "rainy"}},
		"parameters": map[string]interface{}{
			"text_type":   "query",
			"dimension":   float64(768),
			"output_type": AlibabaEmbeddingOutputDenseSparse,
		},
	}
	if !reflect.DeepEqual(capturedBody, wantBody) {
		t.Fatalf("body = %#v, want %#v", capturedBody, wantBody)
	}
	if !reflect.DeepEqual(result.Embeddings, [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}}) {
		t.Fatalf("embeddings = %#v", result.Embeddings)
	}
	if result.Usage.Tokens != 8 || result.Usage.TotalTokens != 8 || len(result.Responses) != 1 || result.Responses[0].Headers["X-Request-Id"] != "emb-1" {
		t.Fatalf("metadata usage=%#v responses=%#v", result.Usage, result.Responses)
	}
	if body, ok := result.Responses[0].Body.(map[string]interface{}); !ok || body["output"] == nil {
		t.Fatalf("response body = %#v, want parsed JSON object", result.Responses[0].Body)
	}
	alibabaMeta, ok := result.ProviderMetadata["alibaba"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata = %#v", result.ProviderMetadata)
	}
	sparse, ok := alibabaMeta["sparseEmbeddings"].([]map[string]interface{})
	if !ok || len(sparse) != 2 {
		t.Fatalf("sparse metadata = %#v", alibabaMeta["sparseEmbeddings"])
	}
	if item := sparse[0]["sparseEmbedding"].([]map[string]interface{})[0]; item["ignored"] != nil {
		t.Fatalf("unknown sparse embedding fields were not stripped: %#v", item)
	}
}

func TestAlibabaEmbeddingModelPreservesNumericDimensionAndOptionalUsage(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"embeddings":[{"embedding":[0.1],"text_index":0}]}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", EmbeddingBaseURL: server.URL})
	model := NewEmbeddingModel(p, AlibabaEmbeddingTextV4)
	_, err := model.DoEmbedMany(context.Background(), []string{"sunny"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{"dimension": float64(0)},
		},
	})
	if err != nil {
		t.Fatalf("DoEmbedMany: %v", err)
	}
	parameters := capturedBody["parameters"].(map[string]interface{})
	if _, ok := parameters["dimension"]; !ok || parameters["dimension"] != float64(0) {
		t.Fatalf("dimension = %#v, want explicit 0", parameters["dimension"])
	}

	fractionalDimension := 1536.5
	_, err = model.DoEmbedMany(context.Background(), []string{"sunny"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"alibaba": AlibabaEmbeddingModelOptions{Dimension: &fractionalDimension},
		},
	})
	if err != nil {
		t.Fatalf("DoEmbedMany fractional dimension: %v", err)
	}
	parameters = capturedBody["parameters"].(map[string]interface{})
	if parameters["dimension"] != fractionalDimension {
		t.Fatalf("dimension = %#v, want fractional number %v", parameters["dimension"], fractionalDimension)
	}
}

func TestAlibabaEmbeddingSingleAndValidation(t *testing.T) {
	p := New(Config{APIKey: "test-api-key", EmbeddingBaseURL: "http://127.0.0.1:1"})
	model := NewEmbeddingModel(p, AlibabaEmbeddingTextV4)

	if _, err := model.DoEmbedMany(context.Background(), make([]string, 11), nil); !providererrors.IsTooManyEmbeddingValuesForCallError(err) {
		t.Fatalf("expected too many values error, got %T %v", err, err)
	}
	if _, err := model.DoEmbedMany(context.Background(), []string{"x"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"outputType": AlibabaEmbeddingOutputSparse}},
	}); !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("expected sparse-only unsupported error, got %T %v", err, err)
	}
	if _, err := model.DoEmbedMany(context.Background(), []string{"x"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"textType": "bad"}},
	}); err == nil {
		t.Fatal("expected invalid textType error")
	} else {
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) {
			t.Fatalf("expected InvalidArgumentError, got %T %v", err, err)
		}
		if invalid.Field != "providerOptions" || invalid.Message != "invalid alibaba provider options" {
			t.Fatalf("invalid argument error = %#v", invalid)
		}
	}
}

func TestAlibabaEmbeddingModelParsesProviderErrorPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "bad-1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"InvalidParameter","message":"bad embedding request","request_id":"bad-1"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", EmbeddingBaseURL: server.URL})
	model := NewEmbeddingModel(p, AlibabaEmbeddingTextV4)
	_, err := model.DoEmbedMany(context.Background(), []string{"x"}, nil)
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T %v", err, err)
	}
	if providerErr.StatusCode != http.StatusBadRequest || providerErr.ErrorCode != "InvalidParameter" || providerErr.Message != "bad embedding request" {
		t.Fatalf("provider error = %#v", providerErr)
	}
	if providerErr.ResponseHeaders["X-Request-Id"] != "bad-1" {
		t.Fatalf("response headers = %#v", providerErr.ResponseHeaders)
	}
}
