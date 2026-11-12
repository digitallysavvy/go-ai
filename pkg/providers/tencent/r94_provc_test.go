package tencent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const r94provcBOM = "\xEF\xBB\xBF"

// TS createJsonResponseHandler reads response.text() (TextDecoder drops a
// UTF-8 BOM): BOM-prefixed TokenHub and VOD bodies parse.
func TestR94provcResponseBodyIsText(t *testing.T) {
	s := newVideoServer(t)
	s.set("/v1/wand/kling/text-to-video", 200, r94provcBOM+submittedResponse)
	s.set("/v1/wand/kling/tasks/"+testTaskID, 200, r94provcBOM+succeededStatus)
	s.set("/", 200, r94provcBOM+`{"Response":{"TaskId":"task-123","RequestId":"req-vod-1"}}`)
	m := s.model(t, "kling-video-v3")
	if _, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()}); err != nil {
		t.Fatalf("tokenhub start: %v", err)
	}
	res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"taskId":"task-123"}`)})
	if err != nil || res.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("tokenhub status: %v %+v", err, res)
	}
	if _, err := s.model(t, "vod/VS/2.0").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()}); err != nil {
		t.Fatalf("vod start: %v", err)
	}
}
