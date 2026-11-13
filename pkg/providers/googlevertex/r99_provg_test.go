package googlevertex

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS combineHeaders(config.headers(), options.headers) for the Vertex Live
// handshake: config.headers() is withUserAgentSuffix({Authorization,
// ...options.headers}), so its keys are lower case, and the ws handshake sets
// the headers in entry order, case-insensitively: a call header in another
// case replaces the provider's (user-agent, authorization, a config header),
// and a config header replaces the bearer token. Go copied the maps by hand,
// so both spellings reached the dialer and map order picked one.
func TestR99provgVertexLiveHandshakeHeaderOverridesFoldCase(t *testing.T) {
	for i := 0; i < 12; i++ {
		server := newLiveTranscriptionTestServer(t)
		prov, err := New(Config{
			Project: "test-project", Location: "us-central1", AccessToken: "test-oauth-token", BaseURL: server.ts.URL,
			Headers: map[string]string{"x-goog-user-project": "config-project", "x-config-only": "kept"},
		})
		if err != nil {
			t.Fatal(err)
		}
		prov.liveWebSocketOrigin = "ws" + strings.TrimPrefix(server.ts.URL, "http")
		model := NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)
		result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newChanAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			Headers: map[string]string{
				"User-Agent":          "caller-ua",
				"Authorization":       "Bearer caller-token",
				"X-Goog-User-Project": "call-project",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
		server.mu.Lock()
		h := server.handshakeHeaders.Clone()
		server.mu.Unlock()
		_ = result.Stream.Close()
		server.close()
		if got := h.Values("User-Agent"); len(got) != 1 || got[0] != "caller-ua" {
			t.Fatalf("run %d: User-Agent = %q, want the call header", i, got)
		}
		if got := h.Get("Authorization"); got != "Bearer caller-token" {
			t.Fatalf("run %d: Authorization = %q, want the call header", i, got)
		}
		if got := h.Get("X-Goog-User-Project"); got != "call-project" {
			t.Fatalf("run %d: X-Goog-User-Project = %q, want the call header", i, got)
		}
		if got := h.Get("X-Config-Only"); got != "kept" {
			t.Fatalf("run %d: X-Config-Only = %q", i, got)
		}
	}
}

// A config Authorization header replaces the resolved bearer token (TS
// {Authorization: Bearer ..., ...options.headers}), whatever its case.
func TestR99provgVertexLiveConfigAuthorizationWins(t *testing.T) {
	for i := 0; i < 12; i++ {
		server := newLiveTranscriptionTestServer(t)
		prov, err := New(Config{
			Project: "test-project", Location: "us-central1", AccessToken: "test-oauth-token", BaseURL: server.ts.URL,
			Headers: map[string]string{"authorization": "Bearer proxy-token"},
		})
		if err != nil {
			t.Fatal(err)
		}
		prov.liveWebSocketOrigin = "ws" + strings.TrimPrefix(server.ts.URL, "http")
		model := NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)
		result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newChanAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
		got := server.authorizationHeader()
		_ = result.Stream.Close()
		server.close()
		if got != "Bearer proxy-token" {
			t.Fatalf("run %d: Authorization = %q, want the config header", i, got)
		}
	}
}
