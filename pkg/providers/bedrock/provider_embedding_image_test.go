package bedrock

import (
	"context"
	"errors"
	"io"
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
	if CreateAmazonBedrock(Config{Region: "us-east-1"}).Name() != "aws-bedrock" || p.Name() != "aws-bedrock" {
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
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking")
	}
	if im, err := p.ImageModel(""); err != nil || im.ModelID() != "stability.stable-diffusion-xl-v1" {
		t.Fatalf("default image model mismatch model=%#v err=%v", im, err)
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "env-ak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-sk")
	t.Setenv("AWS_SESSION_TOKEN", "env-st")
	creds, err := p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "env-ak" {
		t.Fatalf("resolveCredentials env mismatch creds=%#v err=%v", creds, err)
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

func TestBedrockEmbeddingModelDoEmbedAndDoEmbedMany(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			if strings.Contains(req.URL.Path, "cohere.embed") {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"1"}},
					Body:       io.NopCloser(strings.NewReader(`{"embeddings":[[1,2,3]]}`)),
				}, nil
			}
			_ = body
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Req": []string{"2"}},
				Body:       io.NopCloser(strings.NewReader(`{"embedding":[0.1,0.2]}`)),
			}, nil
		})},
	})

	cohere := NewEmbeddingModel(p, "cohere.embed-v4")
	if cohere.SpecificationVersion() != "v3" || cohere.Provider() != "aws-bedrock" || cohere.ModelID() != "cohere.embed-v4" {
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
}

func TestBedrockImageModelHelpers(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	model := NewImageModel(p, "stability.stable-diffusion-xl-v1")
	if model.SpecificationVersion() != "v4" || model.Provider() != "aws-bedrock" || model.ModelID() != "stability.stable-diffusion-xl-v1" {
		t.Fatalf("image model metadata mismatch")
	}
	n := 2
	body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "cat", Size: "640x480", N: &n})
	if body["samples"] != 2 || body["width"] != 640 || body["height"] != 480 {
		t.Fatalf("buildRequestBody mismatch: %#v", body)
	}
	img, err := model.convertResponse([]byte(`{"artifacts":[{"base64":"abc","finishReason":"SUCCESS"}]}`))
	if err != nil || string(img.Image) != "abc" {
		t.Fatalf("convertResponse mismatch result=%#v err=%v", img, err)
	}
	if _, err := model.convertResponse([]byte(`{"artifacts":[]}`)); err == nil {
		t.Fatal("expected no images error")
	}
}

func TestBedrockImageAndLanguageDoGenerateAndStream(t *testing.T) {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/model/stability.stable-diffusion-xl-v1/invoke") {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"X-Req": []string{"img"}},
					Body:       io.NopCloser(strings.NewReader(`{"artifacts":[{"base64":"imgdata","finishReason":"SUCCESS"}]}`)),
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

	imageModel, _ := p.ImageModel("")
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
	if lm.SpecificationVersion() != "v4" || lm.Provider() != "aws-bedrock" || lm.ModelID() != "anthropic.claude" {
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
