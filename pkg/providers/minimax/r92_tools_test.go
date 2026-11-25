package minimax

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS createMiniMax getMediaHeaders resolves `apiKey: Resolvable<string>` for
// the image model too: Config.APIKeyFunc is called per image request and its
// key is the Bearer token; the generated image download gets no credentials.
func TestR92toolsMiniMaxImageAPIKeyFunc(t *testing.T) {
	srv, calls, mu := r91toolsImageServer(t, func() (int, string) {
		return 200, `{"data":[{"url":"IMAGE_URL"}]}`
	}, nil)
	t.Setenv("MINIMAX_API_KEY", "env-key")
	resolved := 0
	p := New(Config{ImageBaseURL: srv.URL + "/v1", APIKeyFunc: func(ctx context.Context) (string, error) {
		resolved++
		return "func-key", nil
	}})
	m, _ := p.Image(ModelH3Image)
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: r91toolsN(1)}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if resolved != 1 || (*calls)[0].headers.Get("Authorization") != "Bearer func-key" {
		t.Fatalf("resolved = %d, authorization = %q", resolved, (*calls)[0].headers.Get("Authorization"))
	}
	if (*calls)[1].headers.Get("Authorization") != "" {
		t.Fatalf("download authorization = %q", (*calls)[1].headers.Get("Authorization"))
	}
}

// TS getStatus usage metadata {totalSeconds: task.usage.total_seconds, ...}:
// a null wire member stays null, an absent one is left out.
func TestR92toolsMiniMaxVideoUsageNullMembers(t *testing.T) {
	status := `{"task":{"id":"task-123","status":"succeeded","content":{"url":"https://cdn.minimax.io/v.mp4"},"usage":{"total_seconds":5,"input_seconds":null}}}`
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{status})
	defer server.Close()
	model := newVideoModel(providerForMinimaxServer(server.URL), ModelH3)
	op, _ := json.Marshal(minimaxOperation{TaskID: minimaxTestTaskID})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	usage := result.ProviderMetadata["minimax"].(map[string]interface{})["usage"]
	if want := map[string]interface{}{"totalSeconds": 5.0, "inputSeconds": nil}; !reflect.DeepEqual(usage, want) {
		t.Fatalf("usage = %#v, want %#v", usage, want)
	}
}
