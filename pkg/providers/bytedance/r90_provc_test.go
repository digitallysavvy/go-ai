package bytedance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provcVideoErrorTextIsTSMessage: TS doStart / doStatus throw
// AISDKError({name: 'BYTEDANCE_VIDEO_GENERATION_ERROR', message})
// (bytedance-video-model.ts 404-409, 437-442), so the error text is the
// message alone. Go wrapped it as "bytedance error (code 500): Video
// generation failed - <message>". The *Error type still matches.
func TestR90provcVideoErrorTextIsTSMessage(t *testing.T) {
	t.Run("no task id", func(t *testing.T) {
		ts := newTestServer(t, "", []string{`{}`})
		defer ts.server.Close()
		m := newVideoModel(providerForServer(t, ts.server.URL), "seedance-1-0-pro-250528")
		_, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
		var be *Error
		if !errors.As(err, &be) || err.Error() != "No task ID returned from API" {
			t.Fatalf("err = %v, want the bare TS message", err)
		}
	})
	t.Run("no video url", func(t *testing.T) {
		ts := newTestServer(t, "task-1", []string{`{"id":"task-1","status":"succeeded","content":{}}`})
		defer ts.server.Close()
		m := newVideoModel(providerForServer(t, ts.server.URL), "seedance-1-0-pro-250528")
		op, _ := json.Marshal(bytedanceOperation{TaskID: "task-1"})
		_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
		var be *Error
		if !errors.As(err, &be) || err.Error() != "No video URL in response. Task ID: task-1" {
			t.Fatalf("err = %v, want the bare TS message", err)
		}
	})
}
