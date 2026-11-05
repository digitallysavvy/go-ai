package youcom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

func r87eprovbKey(s string) *string { return &s }

// TS plugin 3.1.2 createYouClient({apiKey = process.env.YDC_API_KEY}): the
// default applies only to an undefined apiKey, and the header is sent only
// for a truthy key, so an explicit empty apiKey sends no Authorization header even
// with YDC_API_KEY set.
func TestR87eprovbCreateYouClientExplicitEmptyKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		key      *string
		wantAuth string
	}{
		{"nil uses the env", nil, "Bearer env-key"},
		{"explicit empty", r87eprovbKey(""), ""},
		{"explicit key", r87eprovbKey("k2"), "Bearer k2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YDC_API_KEY", "env-key")
			s := &r87cprovbMCPServer{}
			srv := httptest.NewServer(http.HandlerFunc(s.handler))
			defer srv.Close()
			client, err := CreateYouClient(context.Background(), YouClientConfig{APIKey: tc.key, URL: srv.URL + "/mcp"})
			if err != nil {
				t.Fatalf("CreateYouClient: %v", err)
			}
			defer func() { _ = client.Close() }()
			if _, err := mcp.GetMCPToolsForAgent(context.Background(), client); err != nil {
				t.Fatalf("tools: %v", err)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			for i, auth := range s.auths {
				if auth != tc.wantAuth {
					t.Errorf("request %d auth %q, want %q", i, auth, tc.wantAuth)
				}
			}
		})
	}
}
