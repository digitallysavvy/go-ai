package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
	"github.com/digitallysavvy/go-ai/pkg/providers/heygen"
	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createJsonResponseHandler on the task-creation body of the async video
// providers: a body that does not match the schema is "Invalid JSON response".
func TestVideoTaskCreationResponseSchemaValidation(t *testing.T) {
	type tc struct {
		name string
		body string
		make func(u string) (provider.VideoModelV3, error)
	}
	cases := []tc{
		{"klingai", `{"message":"ok"}`, func(u string) (provider.VideoModelV3, error) {
			p, err := klingai.New(klingai.Config{APIKey: "k", BaseURL: u})
			if err != nil {
				return nil, err
			}
			return p.VideoModel("kling-v2.6-t2v")
		}},
		{"minimax", `{"task_id":5}`, func(u string) (provider.VideoModelV3, error) {
			return minimax.New(minimax.Config{APIKey: "k", VideoBaseURL: u}).VideoModel("MiniMax-Hailuo-2.3")
		}},
		{"bytedance", `{"id":5}`, func(u string) (provider.VideoModelV3, error) {
			p, err := bytedance.New(bytedance.Config{APIKey: "k", BaseURL: u})
			if err != nil {
				return nil, err
			}
			return p.VideoModel("seedance-1-0-pro-250528")
		}},
		{"alibaba", `{"output":{"task_id":"t"}}`, func(u string) (provider.VideoModelV3, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", VideoBaseURL: u}).VideoModel("wan2.6-t2v")
		}},
		{"xai", `{"request_id":5}`, func(u string) (provider.VideoModelV3, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: u}).VideoModel("grok-imagine-video")
		}},
		{"heygen", `{"data":{"video_id":""}}`, func(u string) (provider.VideoModelV3, error) {
			return heygen.New(heygen.Config{APIKey: "k", BaseURL: u}).VideoModel("heygen-video")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			m, err := c.make(srv.URL)
			if err != nil {
				t.Skipf("model not constructible: %v", err)
			}
			starter, ok := m.(interface {
				DoStart(context.Context, *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error)
			})
			if !ok {
				t.Skip("no DoStart")
			}
			_, err = starter.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
				VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "a cat", PromptSet: true, N: 1}})
			if !providerutils.IsInvalidJSONResponse(err) {
				t.Fatalf("error = %v, want Invalid JSON response", err)
			}
		})
	}
}
