package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sigv4check"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func signedProvider(srvURL string) *Provider {
	return New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret-key", Region: "us-east-1", BaseURL: srvURL})
}

// TS reranking fixture through the agent-runtime /rerank route: request shape
// (modelArn, numberOfResults, INLINE sources), a signature AWS accepts, and
// the ranking order as returned.
func TestRerankFixtureRequestAndSignature(t *testing.T) {
	var mu sync.Mutex
	var body map[string]interface{}
	var reason, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		reason = sigv4check.Verify(r, raw, "secret-key", "us-east-1", "bedrock")
		path = r.URL.Path
		_ = json.Unmarshal(raw, &body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bedrockFixture(t, "amazon-bedrock-reranking.1.json"))
	}))
	defer srv.Close()

	m, err := signedProvider(srv.URL).RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	topN := 2
	res, err := m.DoRerank(context.Background(), &provider.RerankOptions{
		Query: "best fruit", Documents: []string{"apple", "banana"}, TopN: &topN,
		ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"nextToken": "tok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if reason != "" {
		t.Fatalf("signature rejected: %s", reason)
	}
	if path != "/rerank" {
		t.Fatalf("path = %s", path)
	}
	cfg := body["rerankingConfiguration"].(map[string]interface{})["bedrockRerankingConfiguration"].(map[string]interface{})
	if cfg["modelConfiguration"].(map[string]interface{})["modelArn"] != "arn:aws:bedrock:us-east-1::foundation-model/amazon.rerank-v1:0" || cfg["numberOfResults"] != float64(2) || body["nextToken"] != "tok" {
		t.Fatalf("body = %#v", body)
	}
	src := body["sources"].([]interface{})[0].(map[string]interface{})["inlineDocumentSource"].(map[string]interface{})
	if src["type"] != "TEXT" || src["textDocument"].(map[string]interface{})["text"] != "apple" {
		t.Fatalf("sources = %#v", body["sources"])
	}
	if len(res.Ranking) != 2 || res.Ranking[0].Index != 0 || res.Ranking[1].Index != 5 || res.Ranking[1].RelevanceScore != 0.30241215229034424 {
		t.Fatalf("ranking = %+v", res.Ranking)
	}
}

// Embedding / image / rerank failures are ProviderErrors with the Bedrock
// message, so embed/embedMany/generateImage retry throttling and 5xx.
func TestBedrockModelHTTPErrorsAreRetryableProviderErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-amzn-errortype", "ThrottlingException")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many requests, please wait before trying again."}`))
	}))
	defer srv.Close()
	p := signedProvider(srv.URL)
	em, _ := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	im, _ := p.ImageModel("amazon.nova-canvas-v1:0")
	rm, _ := p.RerankingModel("amazon.rerank-v1:0")
	for name, call := range map[string]func() error{
		"embed": func() error { _, err := em.DoEmbed(context.Background(), "x", nil); return err },
		"embedMany": func() error {
			_, err := em.DoEmbedMany(context.Background(), []string{"x", "y"}, nil)
			return err
		},
		"image": func() error {
			_, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
			return err
		},
		"rerank": func() error {
			_, err := rm.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
			return err
		},
	} {
		err := call()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || !pe.IsRetryable() || !strings.Contains(pe.Message, "please wait before trying again") {
			t.Errorf("%s: err = %#v", name, err)
		}
	}
}

// Nova Canvas replies: images decode from base64; a moderated request is an
// error naming the reasons; an empty reply says so (TS wording).
func TestImageModelResponses(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG-bytes"))
	replies := []string{
		`{"images":["` + png + `"]}`,
		`{"status":"Request Moderated","details":{"Moderation Reasons":["Derivative Works Filter","Nudity Filter"]}}`,
		`{"status":"Request Moderated","details":{"Moderation Reasons":[]}}`,
		`{"images":[]}`,
		`{"images":[],"status":"Failed"}`,
	}
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replies[i]))
		i++
	}))
	defer srv.Close()
	im, _ := signedProvider(srv.URL).ImageModel("amazon.nova-canvas-v1:0")
	gen := func() (*providererrors.ProviderError, error, int) {
		res, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
		n := 0
		if res != nil {
			n = len(res.Base64Images)
		}
		return nil, err, n
	}
	if _, err, n := gen(); err != nil || n != 1 {
		t.Fatalf("success: n=%d err=%v", n, err)
	}
	wants := []string{
		"Amazon Bedrock request was moderated: Derivative Works Filter, Nudity Filter",
		"Amazon Bedrock request was moderated: ",
		"Amazon Bedrock returned no images. ",
		"Amazon Bedrock returned no images. Status: Failed",
	}
	for _, want := range wants {
		_, err, _ := gen()
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
}

// Cohere / Titan / Nova embedding wire formats through the full path.
func TestEmbeddingWireFormats(t *testing.T) {
	var path string
	var body map[string]interface{}
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	p := signedProvider(srv.URL)

	reply = `{"embedding":[0.1,0.2,0.3],"inputTextTokenCount":4}`
	titan, _ := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	dims := 256
	res, err := titan.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Dimensions: &dims})
	if err != nil {
		t.Fatal(err)
	}
	if body["inputText"] != "hello" || body["dimensions"] != float64(256) || len(res.Embedding) != 3 || res.Usage.Tokens != 4 {
		t.Fatalf("titan: body=%#v res=%+v", body, res)
	}
	if !strings.HasSuffix(path, "/invoke") || !strings.Contains(path, "amazon.titan-embed-text-v2") {
		t.Fatalf("titan path = %s", path)
	}

	reply = `{"embeddings":[[0.5,0.6],[0.7,0.8]],"id":"x","response_type":"embeddings_floats","texts":["a","b"]}`
	cohere, _ := p.EmbeddingModel("cohere.embed-english-v3")
	many, err := cohere.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	texts, _ := body["texts"].([]interface{})
	if len(texts) != 2 || body["input_type"] == nil || len(many.Embeddings) != 2 || many.Embeddings[1][0] != 0.7 {
		t.Fatalf("cohere: body=%#v res=%+v", body, many)
	}
}
