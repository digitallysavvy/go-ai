package heygen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doStatus validates the operation with z.object({videoId, mode,
// resolution}): keys match exactly, so a "videoid" key leaves videoId
// missing and the operation is rejected before any request. encoding/json
// matched it case-insensitively.
func TestR99provgStatusOperationKeysMatchExactly(t *testing.T) {
	model, _ := New(Config{BaseURL: "https://127.0.0.1:1", APIKey: "k"}).VideoModel("heygen-video-1")
	_, err := model.(*VideoModel).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{
		Operation: json.RawMessage(`{"videoid":"v1","mode":"text_to_video","resolution":"768p"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid operation reference") {
		t.Fatalf("err = %v, want the invalid operation reference error", err)
	}
}
