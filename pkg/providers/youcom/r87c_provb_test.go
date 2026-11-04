package youcom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// r87cprovbMCPServer is a minimal hosted MCP server: server/discover is not
// supported (the client falls back to initialize), initialize succeeds and
// lists one tool, notifications are accepted. It records the request URI and
// Authorization header of every POST.
type r87cprovbMCPServer struct {
	mu    sync.Mutex
	uris  []string
	auths []string
}

func (s *r87cprovbMCPServer) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	s.uris = append(s.uris, r.URL.RequestURI())
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var msg struct {
		ID     interface{} `json:"id"`
		Method string      `json:"method"`
	}
	_ = json.Unmarshal(body, &msg)
	if msg.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := map[string]interface{}{"jsonrpc": "2.0", "id": msg.ID}
	switch msg.Method {
	case "initialize":
		reply["result"] = map[string]interface{}{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "you", "version": "1"},
		}
	case "tools/list":
		reply["result"] = map[string]interface{}{"tools": []interface{}{
			map[string]interface{}{"name": "you-search", "description": "Search", "inputSchema": map[string]interface{}{"type": "object"}},
		}}
	default:
		reply["error"] = map[string]interface{}{"code": -32601, "message": "Method not found"}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// CreateYouClient (TS @youdotcom-oss/ai-sdk-plugin 3.1.2 createYouClient):
// `new URL('https://api.you.com/mcp')` with searchParams.set('profile') or,
// without a profile, searchParams.set('tools', tools.join(',')) (an empty
// array is truthy), `Authorization: Bearer <apiKey>` only for a truthy key
// (YDC_API_KEY by default), and an initialized MCP client.
func TestR87cprovbCreateYouClient(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      YouClientConfig
		env      string
		wantURI  string
		wantAuth string
	}{
		{"default", YouClientConfig{}, "", "/mcp", ""},
		{"api key", YouClientConfig{APIKey: "k1"}, "", "/mcp", "Bearer k1"},
		{"env key", YouClientConfig{}, "env-key", "/mcp", "Bearer env-key"},
		{"tools", YouClientConfig{Tools: []string{"you-search", "you-contents"}}, "", "/mcp?tools=you-search%2Cyou-contents", ""},
		{"one tool", YouClientConfig{Tools: []string{"you-finance"}}, "", "/mcp?tools=you-finance", ""},
		{"profile wins", YouClientConfig{Profile: "free", Tools: []string{"you-search"}}, "", "/mcp?profile=free", ""},
		{"empty tools", YouClientConfig{Tools: []string{}}, "", "/mcp?tools=", ""},
		{"URLSearchParams escaping", YouClientConfig{Tools: []string{"a b*~"}}, "", "/mcp?tools=a+b*%7E", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YDC_API_KEY", tc.env)
			s := &r87cprovbMCPServer{}
			srv := httptest.NewServer(http.HandlerFunc(s.handler))
			defer srv.Close()
			cfg := tc.cfg
			cfg.URL = srv.URL + "/mcp"
			client, err := CreateYouClient(context.Background(), cfg)
			if err != nil {
				t.Fatalf("CreateYouClient: %v", err)
			}
			defer func() { _ = client.Close() }()
			tools, err := mcp.GetMCPToolsForAgent(context.Background(), client)
			if err != nil {
				t.Fatalf("tools: %v", err)
			}
			if len(tools) != 1 || tools[0].Name != "you-search" {
				t.Errorf("tools = %v, want [you-search]", tools)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.uris) < 3 { // discover, initialize, initialized, tools/list
				t.Fatalf("server saw %d requests", len(s.uris))
			}
			for i, uri := range s.uris {
				if uri != tc.wantURI || s.auths[i] != tc.wantAuth {
					t.Errorf("request %d: %s auth %q, want %s auth %q", i, uri, s.auths[i], tc.wantURI, tc.wantAuth)
				}
			}
		})
	}
}

// The default endpoint is the hosted server, and a failed handshake returns
// its error (TS createMCPClient awaits init, which closes and rethrows).
func TestR87cprovbCreateYouClientDefaultsAndFailure(t *testing.T) {
	if got, _ := youClientURL(YouClientConfig{}); got != "https://api.you.com/mcp" {
		t.Errorf("default URL %q", got)
	}
	if got, _ := youClientURL(YouClientConfig{Profile: "free"}); got != "https://api.you.com/mcp?profile=free" {
		t.Errorf("profile URL %q", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()
	client, err := CreateYouClient(context.Background(), YouClientConfig{URL: srv.URL + "/mcp", APIKey: "bad"})
	if err == nil || client != nil {
		t.Fatalf("CreateYouClient = %v, %v; want an error", client, err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not name the 401", err)
	}
}
