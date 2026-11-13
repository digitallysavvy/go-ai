package cartesia

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provcRealtimeCombinedDurationOverflow: transcript durations whose
// sum overflows the double range give duration null in the combined raw, as
// JSON.stringify writes Infinity; the raw stays valid JSON.
func TestR96provcRealtimeCombinedDurationOverflow(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	parse(t, m, `{"type":"transcript","request_id":"r","text":"a","is_final":true,"duration":1.7e308}`)
	parse(t, m, `{"type":"transcript","request_id":"r","text":"b","is_final":true,"duration":1.7e308}`)
	e := parse(t, m, `{"type":"flush_done","request_id":"r"}`)
	if len(e) != 2 {
		t.Fatalf("events = %+v", e)
	}
	if !json.Valid(e[1].Raw) || !strings.HasSuffix(string(e[1].Raw), `,"duration":null}`) {
		t.Errorf("raw = %s", e[1].Raw)
	}
}

// TestR96provcRealtimeLenientEvent: TS casts the parsed server event, so a
// field of another type does not drop it: a turn.end with a numeric
// request_id and a string duration still gives speech-stopped and the
// completed transcript.
func TestR96provcRealtimeLenientEvent(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	parse(t, m, `{"type":"connected","request_id":"s"}`)
	e := parse(t, m, `{"type":"turn.end","request_id":7,"transcript":"hi","duration":"x"}`)
	if len(e) != 2 || e[0].Type != "speech-stopped" || e[0].ItemID != "7" || e[1].Type != "input-transcription-completed" || e[1].Transcript != "hi" {
		t.Fatalf("events = %+v", e)
	}
	e = parse(t, m, `{"type":"error","message":5}`)
	if len(e) != 1 || e[0].Type != "error" || e[0].Message != "5" {
		t.Fatalf("events = %+v", e)
	}
}

// TestR96provcClientSecretErrorBodyIsText: TS reads the failed body with
// response.text(), which drops a UTF-8 BOM and replaces invalid bytes.
func TestR96provcClientSecretErrorBodyIsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("\xEF\xBB\xBFbad\xff"))
	}))
	defer srv.Close()
	_, err := newTestRealtimeModel(srv.URL).DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Cartesia realtime client secret request failed: 401 bad\uFFFD" {
		t.Fatalf("err = %v", err)
	}
}

// TestR96provcClientSecretBodyNotJSON: TS response.json() rejects a success
// body that is not JSON with the parser error, and a null body throws reading
// its token; neither is the missing-token error.
func TestR96provcClientSecretBodyNotJSON(t *testing.T) {
	for body, want := range map[string]string{
		"not json":     "invalid character",
		"null":         "Cannot read properties of null (reading 'token')",
		`{"token":""}`: "Cartesia realtime client secret response did not include a token.",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := newTestRealtimeModel(srv.URL).DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
		srv.Close()
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("body %q: err = %v, want prefix %q", body, err, want)
		}
	}
}
