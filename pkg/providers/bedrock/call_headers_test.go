package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS combineHeaders(config.headers(), options.headers): per-call headers reach
// the wire for Converse generate and stream, and are part of the SigV4
// signature.
func TestConverseForwardsPerCallHeaders(t *testing.T) {
	var seen []string
	var signed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Call-Header"))
		signed = append(signed, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"stop"}`))
	}))
	defer server.Close()
	m := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
	opts := &provider.GenerateOptions{
		Prompt:  types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Headers: map[string]string{"X-Call-Header": "yes"},
	}
	_, _ = m.DoGenerate(context.Background(), opts)
	_, _ = m.DoStream(context.Background(), opts)
	if len(seen) != 2 || seen[0] != "yes" || seen[1] != "yes" {
		t.Fatalf("call header not forwarded on generate+stream: %v", seen)
	}
	for _, a := range signed {
		if !strings.Contains(strings.ToLower(a), "x-call-header") {
			t.Errorf("call header not signed: %s", a)
		}
	}
}
