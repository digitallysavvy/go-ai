package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94integRealtimeClientSecretISOString: TS computes
// new Date(Date.now() + expiresAfterSeconds * 1000).toISOString() (and + 30
// min) in JS numbers: expanded years outside 0..9999, RangeError "Invalid
// time value" before any request outside the time range, and no int64
// overflow of the duration (cr-r94-carry-to-provg).
func TestR94integRealtimeClientSecretISOString(t *testing.T) {
	var body map[string]interface{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "token", "expireTime": "2026-06-07T12:00:00Z"})
	}))
	defer srv.Close()
	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}), "gemini-2.0-flash-live-001")
	if strconv.IntSize < 64 {
		t.Skip("ExpiresAfterSeconds is an int; these offsets need 64 bits")
	}
	nowS := time.Now().Unix()

	call := func(seconds64 int64) error {
		seconds := int(seconds64)
		_, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: &seconds})
		return err
	}
	// Year 10000: about 8000 years ahead (time.Duration would overflow).
	if err := call(253402300800 + 86400 - nowS); err != nil {
		t.Fatal(err)
	}
	if got, _ := body["newSessionExpireTime"].(string); !strings.HasPrefix(got, "+010000-01-0") {
		t.Errorf("newSessionExpireTime = %q", got)
	}
	if got, _ := body["expireTime"].(string); !strings.HasPrefix(got, "+010000-01-0") {
		t.Errorf("expireTime = %q", got)
	}
	// Year -1.
	if err := call(-62198755200 + 86400 - nowS); err != nil {
		t.Fatal(err)
	}
	if got, _ := body["newSessionExpireTime"].(string); !strings.HasPrefix(got, "-000001-01-0") {
		t.Errorf("newSessionExpireTime = %q", got)
	}
	// Outside the time range: RangeError, no request.
	before := calls
	if err := call(9_000_000_000_000); err == nil || err.Error() != "Invalid time value" {
		t.Fatalf("err = %v, want Invalid time value", err)
	}
	if calls != before {
		t.Fatal("request sent after the RangeError")
	}
}

// TestR94integRealtimeURLsAreWHATWG: TS getRealtimeBaseURL /
// getRealtimeWebSocketURL / getAuthTokensURL use new URL plus the protocol and
// pathname setters (cr-r94-provb-to-provg). Expected from Node 24.
func TestR94integRealtimeURLsAreWHATWG(t *testing.T) {
	const base = "https://GenerativeLanguage.GoogleAPIs.com:443/x/../v1beta?b=1"
	u, err := googleRealtimeWebSocketURL(base, "P")
	if err != nil || u.String() != "wss://generativelanguage.googleapis.com/ws/P?b=1" {
		t.Errorf("websocket URL = %v, %v", u, err)
	}
	if got, err := googleAuthTokenURL(base, "k"); err != nil || got != "https://generativelanguage.googleapis.com/v1alpha/auth_tokens?b=1?key=k" {
		t.Errorf("auth token URL = %q, %v", got, err)
	}
	if got, err := getLiveTranscriptionWebSocketURL(base, "k"); err != nil || got != "wss://generativelanguage.googleapis.com/ws/"+liveTranscriptionWebSocketPath+"?b=1&key=k" {
		t.Errorf("live transcription URL = %q, %v", got, err)
	}
	for _, f := range []func() (string, error){
		func() (string, error) { return googleWebSocketURL("not a url") },
		func() (string, error) { return googleServerWebSocketURL("not a url") },
		func() (string, error) { return googleAuthTokenURL("not a url", "k") },
		func() (string, error) { return getLiveTranscriptionWebSocketURL("not a url", "k") },
		func() (string, error) { return getLiveSpeechTranslationWebSocketURL("not a url", "k") },
	} {
		if _, err := f(); err == nil || err.Error() != "Invalid URL" {
			t.Errorf("invalid base: err = %v, want Invalid URL", err)
		}
	}
}

// r94integURL adapts the (string, error) URL builders for table tests: an
// error reads as "ERR <text>".
func r94integURL(s string, err error) string {
	if err != nil {
		return "ERR " + err.Error()
	}
	return s
}
