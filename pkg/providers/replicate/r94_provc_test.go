package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS providerMetadata.replicate.metrics is the zod-parsed metrics: an
// explicit null stays null, predict_time null is kept, undeclared keys are
// stripped and an absent member is left out.
func TestR94provcVideoMetricsAsParsed(t *testing.T) {
	for _, tc := range []struct{ metrics, want string }{
		{`,"metrics":null`, `{"metrics":null,"predictionId":"p1","videos":[{"url":"https://x/v.mp4"}]}`},
		{`,"metrics":{"predict_time":null,"extra":1}`, `{"metrics":{"predict_time":null},"predictionId":"p1","videos":[{"url":"https://x/v.mp4"}]}`},
		{`,"metrics":{"predict_time":2.5}`, `{"metrics":{"predict_time":2.5},"predictionId":"p1","videos":[{"url":"https://x/v.mp4"}]}`},
		{``, `{"predictionId":"p1","videos":[{"url":"https://x/v.mp4"}]}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"p1","status":"succeeded","output":"https://x/v.mp4","urls":{"get":"x"}` + tc.metrics + `}`))
		}))
		vm := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "owner/model")
		op, _ := json.Marshal(map[string]string{"getUrl": srv.URL + "/predictions/p1"})
		res, err := vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(res.ProviderMetadata["replicate"])
		if string(got) != tc.want {
			t.Errorf("metrics %q: metadata = %s, want %s", tc.metrics, got, tc.want)
		}
	}
}

// TS getFromApi handlers read the body as text (TextDecoder drops a UTF-8
// BOM): a BOM-prefixed prediction parses, and a BOM-prefixed failed poll
// gives the Replicate error message.
func TestR94provcPollBodyIsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"detail":"bad thing"}`))
			return
		}
		_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"id":"p1","status":"processing","urls":{"get":"x"}}`))
	}))
	defer srv.Close()
	vm := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "owner/model")
	op, _ := json.Marshal(map[string]string{"getUrl": srv.URL + "/ok"})
	res, err := vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("status: %v %+v", err, res)
	}
	op, _ = json.Marshal(map[string]string{"getUrl": srv.URL + "/fail"})
	_, err = vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "bad thing" {
		t.Fatalf("failed poll: err = %v, want the Replicate message", err)
	}
}
