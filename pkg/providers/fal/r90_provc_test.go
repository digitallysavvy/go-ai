package fal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcVideoOperationKeepsWebhookSubmitURL: TS submitToQueue returns
// submitUrl = config.url({path: queuePath}) after the fal_webhook query is
// appended (fal-video-model.ts 211-222), and doStart puts it in the
// operation. Go stored the queue path without the query.
func TestR90provcVideoOperationKeepsWebhookSubmitURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"request_id":   "id",
			"response_url": "http://" + r.Host + "/fal-ai/luma-dream-machine/requests/id",
		})
	})
	model, srv := newTestVideoModel(t, "luma-dream-machine", mux)
	res, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
		WebhookURL:              "https://smee.io/abc123",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	var op falVideoOperation
	if err := json.Unmarshal(res.Operation, &op); err != nil {
		t.Fatalf("operation: %v", err)
	}
	want := srv.URL + "/fal-ai/luma-dream-machine?fal_webhook=https%3A%2F%2Fsmee.io%2Fabc123"
	if op.SubmitURL != want {
		t.Fatalf("submitUrl = %q, want %q", op.SubmitURL, want)
	}
	if !strings.HasSuffix(op.ResponseURL, "/requests/id") {
		t.Fatalf("responseUrl = %q", op.ResponseURL)
	}
}

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of the fal error schema (undeclared keys stripped) for a matching
// body and undefined otherwise; the image model uses its union schema.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	asData := func(err error) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("not a ProviderError: %v", err)
		}
		return perr.Data
	}
	httpErr := func(body string) error {
		return &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}
	}
	want := map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(400)}}
	if got := asData((&SpeechModel{}).handleError(httpErr(`{"error":{"message":"m","code":400,"x":1},"y":2}`))); !reflect.DeepEqual(got, want) {
		t.Errorf("speech data = %#v, want %#v", got, want)
	}
	if got := asData((&TranscriptionModel{}).handleError(httpErr(`{"detail":"nope"}`))); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
	img := &ImageModel{}
	wantDetail := map[string]interface{}{"detail": []interface{}{map[string]interface{}{"loc": []interface{}{"body", "prompt"}, "msg": "required", "type": "missing"}}}
	if got := asData(img.statusError(422, nil, []byte(`{"detail":[{"loc":["body","prompt"],"msg":"required","type":"missing","input":null}]}`))); !reflect.DeepEqual(got, wantDetail) {
		t.Errorf("image validation data = %#v, want %#v", got, wantDetail)
	}
	if got := asData(img.statusError(400, nil, []byte(`{"message":"m","request_id":"r"}`))); !reflect.DeepEqual(got, map[string]interface{}{"message": "m"}) {
		t.Errorf("image message data = %#v", got)
	}
	if got := asData(img.statusError(400, nil, []byte(`{"status":"x"}`))); got != nil {
		t.Errorf("image off-schema data = %#v, want nil", got)
	}
}
