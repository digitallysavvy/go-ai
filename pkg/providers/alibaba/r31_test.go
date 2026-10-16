package alibaba

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS APICallError keeps an empty message from {"error":{"message":""}}.
func TestChatStatusErrorKeepsEmptyMessage(t *testing.T) {
	err := chatStatusError(400, nil, []byte(`{"error":{"message":"","code":"x"}}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("got %#v", perr)
	}
}

// TS isVideoUrl is /\.(mp4|mov)([?#]|$)/i: unanchored, so a query value that
// ends in .mp4 counts, and the extension match is case-insensitive.
func TestAlibabaIsVideoURLMatchesTSRegex(t *testing.T) {
	for url, want := range map[string]bool{
		"https://x/a.mp4":              true,
		"https://x/a.MOV?sig=1":        true,
		"https://x/a.mp4#t=1":          true,
		"https://x/play?file=a.mp4":    true,
		"https://x/a.mp4?x=b.png":      true,
		"https://x/a.mp4x":             false,
		"https://x/a.png?x=b.mp4extra": false,
		"https://x/a.png":              false,
	} {
		if got := alibabaIsVideoURL(url); got != want {
			t.Errorf("alibabaIsVideoURL(%q) = %v, want %v", url, got, want)
		}
	}
}

// TS keeps an empty sparse_embedding array ("== null" is the only skip).
func TestEmbeddingEmptySparseEmbeddingIsKept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"embeddings":[{"embedding":[0.1],"text_index":0,"sparse_embedding":[]}]}}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", EmbeddingBaseURL: server.URL}).EmbeddingModel(AlibabaEmbeddingTextV4)
	res, err := model.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := res.ProviderMetadata["alibaba"].(map[string]interface{})
	sparse, _ := meta["sparseEmbeddings"].([]map[string]interface{})
	if len(sparse) != 1 {
		t.Fatalf("providerMetadata = %#v", res.ProviderMetadata)
	}
}
