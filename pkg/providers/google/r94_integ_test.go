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
