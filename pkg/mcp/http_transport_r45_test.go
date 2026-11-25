package mcp

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TS expireSessionId clears a still-current session id through setSessionId,
// so onSessionIdChange fires with undefined, and fires onSessionExpired even
// when the expired id is no longer current ("should notify and clear the
// session id when POST returns 404").
func TestHTTPTransportExpireSessionIDNotifiesLikeTS(t *testing.T) {
	sse := &sessionSSEClient{notFoundOnSessionID: "expired-session"}
	var changes, expired []string
	transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
		URL:               "http://localhost:9999/mcp",
		SSEClient:         sse,
		InitialSessionID:  "expired-session",
		OnSessionIDChange: func(id string) { changes = append(changes, id) },
		OnSessionExpired:  func(id string) { expired = append(expired, id) },
	})
	transport.connected = true

	msg, _ := CreateRequest(1, "tools/list", nil)
	if err := transport.Send(context.Background(), msg); err == nil {
		t.Fatal("expected an error for the 404 response")
	}
	if len(changes) != 1 || changes[0] != "" {
		t.Fatalf("OnSessionIDChange calls = %q, want one call with \"\"", changes)
	}
	if len(expired) != 1 || expired[0] != "expired-session" {
		t.Fatalf("OnSessionExpired calls = %q", expired)
	}

	// A stale id that is no longer current still reports expiry but leaves
	// the current session alone.
	transport.setSessionID("fresh")
	changes = nil
	transport.expireSessionID("expired-session")
	if transport.SessionID() != "fresh" {
		t.Fatalf("SessionID() = %q, want fresh", transport.SessionID())
	}
	if len(changes) != 0 {
		t.Fatalf("OnSessionIDChange calls = %q, want none", changes)
	}
	if len(expired) != 2 || expired[1] != "expired-session" {
		t.Fatalf("OnSessionExpired calls = %q, want a second call", expired)
	}
}

// TS send() applies the mcp-session-id of every response, a 401 included,
// before it retries, so the retried POST carries the id the 401 assigned.
func TestHTTPTransportSessionIDFrom401IsSentOnRetry(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		if callNumber == 0 {
			r := statusResponse(http.StatusUnauthorized, "")
			r.Header.Set("mcp-session-id", "from-401")
			return r, nil
		}
		r := statusResponse(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		r.Header.Set("Content-Type", "application/json")
		return r, nil
	}
	transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OAuth: &OAuthConfig{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(time.Hour),
			RefreshTokenFunc: func(context.Context, *OAuthConfig) (string, time.Duration, error) {
				return "new", time.Hour, nil
			},
		},
	})
	transport.connected = true

	msg, _ := CreateRequest(1, "tools/list", nil)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if sse.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", sse.callCount())
	}
	if got := sse.callAt(1).Header.Get("mcp-session-id"); got != "from-401" {
		t.Fatalf("retry mcp-session-id = %q, want from-401", got)
	}
}
