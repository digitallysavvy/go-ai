package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestBedrockProviderSurfaceAndCredentialHelpers(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	if CreateAmazonBedrock(Config{Region: "us-east-1"}).Name() != "amazon-bedrock" || p.Name() != "amazon-bedrock" {
		t.Fatalf("provider name mismatch")
	}
	if p.Region() != "us-east-1" || p.Client() == nil {
		t.Fatalf("provider metadata mismatch")
	}
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected empty model error")
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("expected empty model error")
	}
	if _, err := p.EmbeddingModelWithOptions("", nil); err == nil {
		t.Fatal("expected empty model error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription")
	}
	if rm, err := p.RerankingModel("amazon.rerank-v1:0"); err != nil || rm.ModelID() != "amazon.rerank-v1:0" {
		t.Fatalf("reranking model mismatch model=%#v err=%v", rm, err)
	}
	if im, err := p.ImageModel(""); err != nil || im.ModelID() != "" {
		t.Fatalf("image model empty-id parity mismatch model=%#v err=%v", im, err)
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "env-ak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-sk")
	t.Setenv("AWS_SESSION_TOKEN", "env-st")
	creds, err := p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "a" || creds.SecretAccessKey != "b" || creds.SessionToken != "" {
		t.Fatalf("resolveCredentials config precedence mismatch creds=%#v err=%v", creds, err)
	}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "credentials")
	if err := os.WriteFile(path, []byte("[default]\naws_access_key_id=ak\naws_secret_access_key=sk\naws_session_token=st\n"), 0o644); err != nil {
		t.Fatalf("write credentials file error = %v", err)
	}
	fileCreds, err := sharedFileCredentials(path, "default")
	if err != nil || fileCreds.AccessKeyID != "ak" || fileCreds.SecretAccessKey != "sk" {
		t.Fatalf("sharedFileCredentials mismatch creds=%#v err=%v", fileCreds, err)
	}
	missingCreds, err := sharedFileCredentials(filepath.Join(tmp, "missing"), "default")
	if err != nil || missingCreds.AccessKeyID != "" {
		t.Fatalf("expected no creds on missing file, got creds=%#v err=%v", missingCreds, err)
	}
	if _, err := instanceRoleCredentials(context.Background()); err == nil {
		t.Fatal("expected IMDS failure in test env")
	}
}

func TestBedrockRerankingModelDoRerank(t *testing.T) {
	p := New(Config{
		Region:             "us-east-1",
		AWSAccessKeyID:     "a",
		AWSSecretAccessKey: "b",
		Headers:            map[string]string{"X-Config": "cfg"},
	})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-agent-runtime.us-east-1.amazonaws.com",
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://bedrock-agent-runtime.us-east-1.amazonaws.com/rerank" {
				t.Fatalf("rerank URL = %s", req.URL.String())
			}
			if req.Header.Get("X-Config") != "cfg" || req.Header.Get("X-Request") != "req" {
				t.Fatalf("headers missing: %#v", req.Header)
			}
			if !strings.Contains(req.Header.Get("User-Agent"), "go-ai/amazon-bedrock/") {
				t.Fatalf("user-agent missing provider suffix: %#v", req.Header)
			}
			body, _ := io.ReadAll(req.Body)
			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload["nextToken"] != "next-1" {
				t.Fatalf("nextToken mismatch: %#v", payload)
			}
			queries := payload["queries"].([]interface{})
			query := queries[0].(map[string]interface{})
			if query["type"] != "TEXT" || query["textQuery"].(map[string]interface{})["text"] != "capital" {
				t.Fatalf("query mismatch: %#v", query)
			}
			config := payload["rerankingConfiguration"].(map[string]interface{})
			if config["type"] != "BEDROCK_RERANKING_MODEL" {
				t.Fatalf("reranking type mismatch: %#v", config)
			}
			amazonConfig := config["amazonBedrockRerankingConfiguration"].(map[string]interface{})
			if amazonConfig["numberOfResults"] != float64(1) {
				t.Fatalf("numberOfResults mismatch: %#v", amazonConfig)
			}
			modelConfig := amazonConfig["modelConfiguration"].(map[string]interface{})
			if modelConfig["modelArn"] != "arn:aws:bedrock:us-east-1::foundation-model/amazon.rerank-v1:0" {
				t.Fatalf("modelArn mismatch: %#v", modelConfig)
			}
			if modelConfig["additionalModelRequestFields"].(map[string]interface{})["foo"] != "bar" {
				t.Fatalf("additional fields mismatch: %#v", modelConfig)
			}
			sources := payload["sources"].([]interface{})
			inline := sources[0].(map[string]interface{})["inlineDocumentSource"].(map[string]interface{})
			if inline["type"] != "TEXT" || inline["textDocument"].(map[string]interface{})["text"] != "Paris" {
				t.Fatalf("source mismatch: %#v", inline)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Req": []string{"rerank"}},
				Body:       io.NopCloser(strings.NewReader(`{"results":[{"index":0,"relevanceScore":0.9}],"nextToken":"next-2"}`)),
			}, nil
		})},
	})

	model := NewRerankingModel(p, "amazon.rerank-v1:0")
	if model.SpecificationVersion() != "v4" || model.Provider() != "amazon-bedrock" {
		t.Fatalf("rerank metadata mismatch")
	}
	topN := 1
	result, err := model.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "capital",
		Documents: []string{"Paris", "Berlin"},
		TopN:      &topN,
		Headers:   map[string]string{"X-Request": "req"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"nextToken":                    "next-1",
				"additionalModelRequestFields": map[string]interface{}{"foo": "bar"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoRerank error = %v", err)
	}
	if len(result.Ranking) != 1 || result.Ranking[0].Index != 0 || result.Ranking[0].RelevanceScore != 0.9 {
		t.Fatalf("ranking mismatch: %#v", result.Ranking)
	}
	if http.Header(result.Response.Headers).Get("X-Req") != "rerank" {
		t.Fatalf("response headers mismatch: %#v", result.Response.Headers)
	}
	if !result.Response.Timestamp.IsZero() || result.Response.ModelID != "" {
		t.Fatalf("rerank response metadata should match TS headers/body only, got %#v", result.Response)
	}
}

func TestBedrockRerankingOptionsPreferAmazonBedrockNamespace(t *testing.T) {
	opts := bedrockRerankingOptions(map[string]interface{}{
		"amazonBedrock": map[string]interface{}{
			"nextToken":                    "current",
			"additionalModelRequestFields": map[string]interface{}{"source": "current"},
		},
		"bedrock": map[string]interface{}{
			"nextToken":                    "legacy",
			"additionalModelRequestFields": map[string]interface{}{"source": "legacy"},
		},
	})
	if opts.NextToken == nil || *opts.NextToken != "current" {
		t.Fatalf("NextToken = %#v, want current namespace precedence", opts.NextToken)
	}
	if opts.AdditionalModelRequestFields["source"] != "current" {
		t.Fatalf("AdditionalModelRequestFields = %#v, want current namespace precedence", opts.AdditionalModelRequestFields)
	}

	legacy := bedrockRerankingOptions(map[string]interface{}{
		"bedrock": map[string]interface{}{
			"nextToken": "legacy",
		},
	})
	if legacy.NextToken == nil || *legacy.NextToken != "legacy" {
		t.Fatalf("legacy NextToken = %#v, want bedrock fallback", legacy.NextToken)
	}

	emptyValues := bedrockRerankingOptions(map[string]interface{}{
		"amazonBedrock": map[string]interface{}{
			"nextToken":                    "",
			"additionalModelRequestFields": map[string]interface{}{},
		},
	})
	if emptyValues.NextToken == nil || *emptyValues.NextToken != "" {
		t.Fatalf("empty NextToken should be preserved when explicitly provided, got %#v", emptyValues.NextToken)
	}
	if !emptyValues.AdditionalModelRequestFieldsSet || len(emptyValues.AdditionalModelRequestFields) != 0 {
		t.Fatalf("empty additionalModelRequestFields should be preserved when explicitly provided, got %#v", emptyValues)
	}

	model := NewRerankingModel(New(Config{Region: "us-east-1"}), "amazon.rerank-v1:0")
	body, err := model.buildRequestBody(&provider.RerankOptions{
		Query:     "capital",
		Documents: []string{"Paris"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"nextToken":                    "",
				"additionalModelRequestFields": map[string]interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}
	if nextToken, ok := body["nextToken"].(string); !ok || nextToken != "" {
		t.Fatalf("empty nextToken should be serialized when explicitly provided: %#v", body)
	}
	config := body["rerankingConfiguration"].(map[string]interface{})
	amazonConfig := config["amazonBedrockRerankingConfiguration"].(map[string]interface{})
	modelConfig := amazonConfig["modelConfiguration"].(map[string]interface{})
	if additional, ok := modelConfig["additionalModelRequestFields"].(map[string]interface{}); !ok || len(additional) != 0 {
		t.Fatalf("empty additionalModelRequestFields should be serialized when explicitly provided: %#v", body)
	}
}

func TestBedrockEmbeddingModelDoEmbedAndDoEmbedMany(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			if strings.Contains(req.URL.Path, "cohere.embed") {
				var payload map[string]interface{}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("decode cohere request: %v", err)
				}
				if payload["input_type"] != "search_query" {
					t.Fatalf("cohere input_type mismatch: %#v", payload)
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"1"}},
					Body:       io.NopCloser(strings.NewReader(`{"embeddings":[[1,2,3]]}`)),
				}, nil
			}
			if strings.Contains(req.URL.Path, "amazon.nova") {
				var payload map[string]interface{}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("decode nova request: %v", err)
				}
				params := payload["singleEmbeddingParams"].(map[string]interface{})
				if payload["taskType"] != "SINGLE_EMBEDDING" || params["embeddingPurpose"] != "TEXT_RETRIEVAL" || params["embeddingDimension"] != float64(384) {
					t.Fatalf("nova request mismatch: %#v", payload)
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"3"}},
					Body:       io.NopCloser(strings.NewReader(`{"embeddings":[{"embeddingType":"FLOAT","embedding":[4,5,6]}],"inputTokenCount":7}`)),
				}, nil
			}
			_ = body
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Req": []string{"2"}},
				Body:       io.NopCloser(strings.NewReader(`{"embedding":[0.1,0.2],"inputTextTokenCount":9}`)),
			}, nil
		})},
	})

	cohere := NewEmbeddingModel(p, "cohere.embed-v4")
	if cohere.SpecificationVersion() != "v4" || cohere.Provider() != "amazon-bedrock" || cohere.ModelID() != "cohere.embed-v4" {
		t.Fatalf("cohere embedding metadata mismatch")
	}
	if cohere.MaxEmbeddingsPerCall() != 1 || !cohere.SupportsParallelCalls() {
		t.Fatalf("cohere limits mismatch")
	}
	one, err := cohere.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "1"}})
	if err != nil || len(one.Embedding) != 3 {
		t.Fatalf("cohere DoEmbed mismatch result=%#v err=%v", one, err)
	}

	titan := NewEmbeddingModel(p, "amazon.titan-embed-text-v2")
	many, err := titan.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil || len(many.Embeddings) != 2 {
		t.Fatalf("titan DoEmbedMany mismatch result=%#v err=%v", many, err)
	}
	if many.Usage.InputTokens != 18 || many.Usage.Tokens != 18 || many.Warnings == nil || len(many.Warnings) != 0 {
		t.Fatalf("titan token usage should use provider token count, got %#v", many.Usage)
	}

	dimension := 384
	nova := NewEmbeddingModel(p, "amazon.nova-embed-text-v1:0", &EmbeddingOptions{NovaOptions: &NovaEmbeddingOptions{
		EmbeddingDimension: &dimension,
		EmbeddingPurpose:   "TEXT_RETRIEVAL",
		Truncate:           "START",
	}})
	novaResult, err := nova.DoEmbed(context.Background(), "hello", nil)
	if err != nil || len(novaResult.Embedding) != 3 || novaResult.Usage.InputTokens != 7 || novaResult.Usage.Tokens != 7 || novaResult.Warnings == nil || len(novaResult.Warnings) != 0 {
		t.Fatalf("nova DoEmbed mismatch result=%#v err=%v", novaResult, err)
	}
}

func TestBedrockEmbeddingCohereV4Response(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Amzn-Bedrock-Input-Token-Count": []string{"6"}},
				Body:       io.NopCloser(strings.NewReader(`{"embeddings":{"float":[[0.4,0.5]]}}`)),
			}, nil
		})},
	})

	cohere := NewEmbeddingModel(p, "cohere.embed-v4")
	result, err := cohere.DoEmbed(context.Background(), "hello", nil)
	if err != nil || len(result.Embedding) != 2 || result.Embedding[0] != 0.4 {
		t.Fatalf("cohere v4 response mismatch result=%#v err=%v", result, err)
	}
	if result.Usage.Tokens != 6 || result.Usage.InputTokens != 6 {
		t.Fatalf("cohere usage = %#v, want header token count 6", result.Usage)
	}
}

func TestBedrockEmbeddingCohereV4MissingTokenHeaderReturnsNaN(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"embeddings":{"float":[[0.4,0.5]]}}`)),
			}, nil
		})},
	})

	cohere := NewEmbeddingModel(p, "cohere.embed-v4:0")
	result, err := cohere.DoEmbed(context.Background(), "hello", nil)
	if err != nil || len(result.Embedding) != 2 {
		t.Fatalf("cohere v4 response mismatch result=%#v err=%v", result, err)
	}
	if !math.IsNaN(result.Usage.Tokens) {
		t.Fatalf("cohere usage tokens = %#v, want NaN when header is absent", result.Usage)
	}
	if result.Usage.InputTokens != 0 || result.Usage.TotalTokens != 0 {
		t.Fatalf("legacy integer usage = %#v, want zero when TS tokens is NaN", result.Usage)
	}
}

func TestBedrockEmbeddingCohereInferenceProfileUsesCohereShape(t *testing.T) {
	var requestBody map[string]interface{}
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			if err := json.Unmarshal(body, &requestBody); err != nil {
				t.Fatalf("decode cohere request: %v", err)
			}
			if !strings.Contains(req.URL.Path, "us.cohere.embed-v4:0") {
				t.Fatalf("path = %q, want inference profile model ID", req.URL.Path)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Amzn-Bedrock-Input-Token-Count": []string{"6"}},
				Body:       io.NopCloser(strings.NewReader(`{"embeddings":{"float":[[0.4,0.5]]}}`)),
			}, nil
		})},
	})

	cohere := NewEmbeddingModel(p, "us.cohere.embed-v4:0")
	result, err := cohere.DoEmbed(context.Background(), "hello", nil)
	if err != nil || len(result.Embedding) != 2 {
		t.Fatalf("cohere inference profile response mismatch result=%#v err=%v", result, err)
	}
	if result.Usage.Tokens != 6 {
		t.Fatalf("cohere inference profile usage = %#v, want header token count 6", result.Usage)
	}
	if requestBody["input_type"] != "search_query" || requestBody["texts"] == nil {
		t.Fatalf("cohere inference profile request used non-cohere shape: %#v", requestBody)
	}
}

func TestBedrockEmbeddingProviderOptionsMatchTS(t *testing.T) {
	var bodies []map[string]interface{}
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode embedding request: %v", err)
			}
			bodies = append(bodies, payload)
			response := `{"embedding":[0.1,0.2],"inputTextTokenCount":1}`
			if strings.Contains(req.URL.Path, "cohere.embed") {
				response = `{"embeddings":[[0.1,0.2]]}`
			}
			if strings.Contains(req.URL.Path, "amazon.nova") {
				response = `{"embeddings":[{"embeddingType":"FLOAT","embedding":[0.1,0.2]}],"inputTokenCount":1}`
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(response)),
			}, nil
		})},
	})

	titan := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0")
	_, err := titan.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"dimensions": 512, "normalize": false},
			"bedrock":       map[string]interface{}{"dimensions": 256, "normalize": true},
		},
	})
	if err != nil {
		t.Fatalf("titan DoEmbed error = %v", err)
	}
	if bodies[0]["dimensions"] != float64(512) || bodies[0]["normalize"] != false {
		t.Fatalf("titan provider options mismatch: %#v", bodies[0])
	}

	cohereModel := NewEmbeddingModel(p, "cohere.embed-v4")
	_, err = cohereModel.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"bedrock": map[string]interface{}{"inputType": "classification", "truncate": "START", "outputDimension": 512},
		},
	})
	if err != nil {
		t.Fatalf("cohere DoEmbed error = %v", err)
	}
	if bodies[1]["input_type"] != "classification" || bodies[1]["truncate"] != "START" || bodies[1]["output_dimension"] != float64(512) {
		t.Fatalf("cohere provider options mismatch: %#v", bodies[1])
	}

	nova := NewEmbeddingModel(p, "amazon.nova-embed-text-v1:0")
	_, err = nova.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"embeddingPurpose": "TEXT_RETRIEVAL", "embeddingDimension": 384, "truncate": "START"},
		},
	})
	if err != nil {
		t.Fatalf("nova DoEmbed error = %v", err)
	}
	params := bodies[2]["singleEmbeddingParams"].(map[string]interface{})
	if params["embeddingPurpose"] != "TEXT_RETRIEVAL" || params["embeddingDimension"] != float64(384) || params["text"].(map[string]interface{})["truncationMode"] != "START" {
		t.Fatalf("nova provider options mismatch: %#v", bodies[2])
	}
}

func TestBedrockImageModelHelpers(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	model := NewImageModel(p, "amazon.nova-canvas-v1:0")
	if model.SpecificationVersion() != "v4" || model.Provider() != "amazon-bedrock" || model.ModelID() != "amazon.nova-canvas-v1:0" {
		t.Fatalf("image model metadata mismatch")
	}
	if model.MaxImagesPerCall() != 5 || NewImageModel(p, "custom").MaxImagesPerCall() != 1 {
		t.Fatalf("MaxImagesPerCall mismatch")
	}
	n := 2
	seed := 7
	body, warnings, err := model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:      "cat",
		Size:        "640x480",
		N:           &n,
		Seed:        &seed,
		AspectRatio: "1:1",
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"quality": "premium", "cfgScale": 7.5, "negativeText": "rain", "style": "photographic"},
			"bedrock":       map[string]interface{}{"negativeText": "legacy"},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}
	if body["taskType"] != "TEXT_IMAGE" || len(warnings) != 1 || warnings[0].Feature != "aspectRatio" {
		t.Fatalf("buildRequestBody mismatch: %#v", body)
	}
	config := body["imageGenerationConfig"].(map[string]interface{})
	if config["numberOfImages"] != 2 || config["width"] != 640 || config["height"] != 480 || config["seed"] != 7 || config["quality"] != "premium" || config["cfgScale"] != 7.5 {
		t.Fatalf("imageGenerationConfig mismatch: %#v", config)
	}
	params := body["textToImageParams"].(map[string]interface{})
	if params["text"] != "cat" || params["negativeText"] != "rain" || params["style"] != "photographic" {
		t.Fatalf("textToImageParams mismatch: %#v", params)
	}
	body, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:  "cat",
		Quality: "should-not-be-sent",
		Style:   "should-not-be-sent",
	})
	if err != nil {
		t.Fatalf("buildRequestBody generic quality/style error = %v", err)
	}
	if _, ok := body["imageGenerationConfig"].(map[string]interface{})["quality"]; ok {
		t.Fatalf("generic quality should not be sent for Bedrock TS parity: %#v", body)
	}
	if _, ok := body["textToImageParams"].(map[string]interface{})["style"]; ok {
		t.Fatalf("generic style should not be sent for Bedrock TS parity: %#v", body)
	}
	body, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "edit",
		Files:  []provider.ImageFile{{Data: []byte("source")}},
		Mask:   &provider.ImageFile{Type: "file", Data: []byte{0xff, 0x00}},
	})
	if err != nil {
		t.Fatalf("buildRequestBody inpainting error = %v", err)
	}
	inpaint := body["inPaintingParams"].(map[string]interface{})
	if body["taskType"] != "INPAINTING" || inpaint["image"] != "c291cmNl" || inpaint["maskImage"] != "/wA=" {
		t.Fatalf("inpainting body mismatch: %#v", body)
	}
	zero := 0
	body, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "zero",
		N:      &zero,
		Seed:   &zero,
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"cfgScale": 0},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody zero options error = %v", err)
	}
	config = body["imageGenerationConfig"].(map[string]interface{})
	if _, ok := config["numberOfImages"]; ok {
		t.Fatalf("numberOfImages=0 should be omitted to match TS truthy spread behavior: %#v", config)
	}
	if _, ok := config["seed"]; ok {
		t.Fatalf("seed=0 should be omitted to match TS truthy spread behavior: %#v", config)
	}
	if _, ok := config["cfgScale"]; ok {
		t.Fatalf("cfgScale=0 should be omitted to match TS truthy spread behavior: %#v", config)
	}
	body, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "mask-data-without-type",
		Files:  []provider.ImageFile{{Data: []byte("source")}},
		Mask:   &provider.ImageFile{Data: []byte{0xff, 0x00}},
	})
	if err != nil {
		t.Fatalf("buildRequestBody data-only mask error = %v", err)
	}
	if body["taskType"] != "IMAGE_VARIATION" {
		t.Fatalf("mask without type should not imply inpainting per TS mask?.type check: %#v", body)
	}
	body, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "variation",
		Files:  []provider.ImageFile{{Data: []byte("c291cmNl")}},
	})
	if err != nil {
		t.Fatalf("buildRequestBody variation error = %v", err)
	}
	variation := body["imageVariationParams"].(map[string]interface{})
	if variation["images"].([]string)[0] != "c291cmNl" {
		t.Fatalf("base64 image should pass through without double encoding: %#v", body)
	}
	img, err := model.convertResponse([]byte(`{"id":"img_1","images":["abc","def"]}`), http.Header{"X-Req": []string{"img"}}, warnings)
	if err != nil || string(img.Image) != "abc" || len(img.Images) != 2 || img.Base64Image != "abc" || img.Response.ModelID != "amazon.nova-canvas-v1:0" || img.Response.Headers["X-Req"] != "img" || len(img.Warnings) != 1 {
		t.Fatalf("convertResponse mismatch result=%#v err=%v", img, err)
	}
	if _, err := model.convertResponse([]byte(`{"images":[],"status":"Done"}`), nil, nil); err == nil {
		t.Fatal("expected no images error")
	}
	if _, err := model.convertResponse([]byte(`{"status":"Request Moderated","details":{"Moderation Reasons":["unsafe"]}}`), nil, nil); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected moderation error, got %v", err)
	}
	_, _, err = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "edit",
		Files:  []provider.ImageFile{{Type: "url", URL: "https://example.com/image.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "URL-based images are not supported") {
		t.Fatalf("expected URL image error, got %v", err)
	}
}

func TestBedrockImageAndLanguageDoGenerateAndStream(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/model/amazon.nova-canvas-v1:0/invoke") {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"img"}},
					Body:       io.NopCloser(strings.NewReader(`{"images":["imgdata"]}`)),
				}, nil
			}
			if strings.Contains(req.URL.Path, "/model/meta.llama-3/invoke") {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"lm"}},
					Body:       io.NopCloser(strings.NewReader(`{"completion":"hello world"}`)),
				}, nil
			}
			return nil, errors.New("unexpected request path: " + req.URL.Path)
		})},
	})

	imageModel, _ := p.ImageModel("amazon.nova-canvas-v1:0")
	img, err := imageModel.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
	if err != nil || string(img.Image) != "imgdata" {
		t.Fatalf("image DoGenerate mismatch result=%#v err=%v", img, err)
	}

	lmAny, err := p.LanguageModel("meta.llama-3")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	lm := lmAny.(*LanguageModel)
	res, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || res.Text != "hello world" {
		t.Fatalf("language DoGenerate mismatch result=%#v err=%v", res, err)
	}
	stream, err := lm.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	gotFinish := false
	for i := 0; i < 10; i++ {
		ch, e := stream.Next()
		if e != nil {
			t.Fatalf("stream next error = %v", e)
		}
		if ch.Type == provider.ChunkTypeFinish {
			gotFinish = true
			break
		}
	}
	if !gotFinish {
		t.Fatal("expected finish chunk")
	}
}

func TestBedrockLanguageHelpersAndStream(t *testing.T) {
	lm := NewLanguageModel(New(Config{Region: "us-east-1"}), "anthropic.claude")
	if lm.SpecificationVersion() != "v4" || lm.Provider() != "amazon-bedrock" || lm.ModelID() != "anthropic.claude" {
		t.Fatalf("language model metadata mismatch")
	}
	if !lm.SupportsTools() || !lm.SupportsStructuredOutput() || !lm.SupportsImageInput() {
		t.Fatalf("capability mismatch")
	}
	if ep := lm.getInvokeEndpoint(); ep != "/model/anthropic.claude/invoke" {
		t.Fatalf("invoke endpoint mismatch: %s", ep)
	}

	usage := convertBedrockUsage(bedrockUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 2, CacheWriteInputTokens: 1})
	if usage.InputDetails == nil || usage.OutputDetails == nil || usage.Raw == nil {
		t.Fatalf("usage conversion mismatch: %#v", usage)
	}

	res, err := lm.convertResponse([]byte(`{"content":[{"type":"text","text":"hello"}],"stop_reason":"max_tokens","usage":{"input_tokens":1,"output_tokens":2}}`))
	if err != nil || res.Text != "hello" || res.FinishReason != "length" {
		t.Fatalf("claude convert mismatch result=%#v err=%v", res, err)
	}
	res2, err := lm.convertResponse([]byte(`{"completion":"world"}`))
	if err != nil || res2.Text != "world" {
		t.Fatalf("generic convert mismatch result=%#v err=%v", res2, err)
	}

	stream := &bedrockStream{result: res2}
	ch1, err := stream.Next()
	if err != nil || ch1.Type != provider.ChunkTypeText {
		t.Fatalf("stream first chunk mismatch chunk=%#v err=%v", ch1, err)
	}
	for i := 0; i < 5; i++ {
		ch, e := stream.Next()
		if e != nil {
			t.Fatalf("unexpected stream err = %v", e)
		}
		if ch.Type == provider.ChunkTypeFinish {
			break
		}
	}
	stream.Close() //nolint:errcheck
	if stream.Err() != nil {
		t.Fatalf("expected nil Err for bedrockStream")
	}
}

func TestReadAndCloseAndResolveCredentialErrors(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("x"))}
	b, err := readAndClose(resp)
	if err != nil || string(b) != "x" {
		t.Fatalf("readAndClose mismatch b=%q err=%v", string(b), err)
	}

	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "only-ak"})
	if _, err := p.resolveCredentials(context.Background()); err == nil {
		t.Fatal("expected incomplete config credentials error")
	}

	// Ensure credential cache reuse path is exercised.
	p2 := New(Config{Region: "us-east-1", AWSAccessKeyID: "ak", AWSSecretAccessKey: "sk"})
	p2.creds = awsCredentials{AccessKeyID: "ak", SecretAccessKey: "sk"}
	p2.credExp = time.Now().Add(time.Hour)
	if _, err := p2.resolveCredentials(context.Background()); err != nil {
		t.Fatalf("cached resolveCredentials error = %v", err)
	}
}

type bedrockRoundTripper func(*http.Request) (*http.Response, error)

func (f bedrockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
