package bfl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// JSON.stringify(detail) normalizes numbers and escapes; it is not a byte-level compact.
func TestR33_DetailIsJSStringified(t *testing.T) {
	err := bflStatusError(422, nil, []byte(`{"detail":{"a":1.0,"b":1e2,"c":"é","a":2}}`))
	if got, want := err.Error(), `{"a":2,"b":100,"c":"é"}`; !contains(got, want) {
		t.Fatalf("got %q want contains %q", got, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestR33_VideoPollDetailsDraftCacheAndTrim(t *testing.T) {
	if got := describePollDetails([]byte(`{"a":1.0,"b":"é"}`)); got != `{"a":1,"b":"é"}` {
		t.Errorf("details = %q", got)
	}

	var statusBody string
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(statusBody))
	})
	defer server.Close()
	op, _ := json.Marshal(bflVideoOperation{RequestID: "r", PollingURL: server.URL + "/get_result"})
	statusBody = `{"status":"Ready","result":{"sample":"https://cdn.bfl.ai/v.mp4","draft_cache":""}}`
	res, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	video := res.ProviderMetadata["blackForestLabs"].(map[string]interface{})["videos"].([]map[string]interface{})[0]
	if v, ok := video["draftCache"]; !ok || v != "" {
		t.Errorf("draftCache = %v (present %v)", v, ok)
	}

	// prompt?.trim() is JS trim: NEL is not whitespace, BOM is.
	draft := "bundle"
	for prompt, want := range map[string]bool{"\u0085": true, "\ufeff ": false} {
		_, warnings := getDraftEnhanceArgs(&provider.VideoModelV3CallOptions{Prompt: prompt}, &VideoModelOptions{DraftCache: &draft})
		got := len(warnings) > 0
		if got != want {
			t.Errorf("prompt %q: warned=%v want %v", prompt, got, want)
		}
	}
}
