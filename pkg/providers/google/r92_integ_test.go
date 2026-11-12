package google

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r92-provg-to-integrator CR 2: TS `new Date(str)` sites parse like
// Date.parse (a date-only string is UTC midnight) and keep millisecond
// precision only.
var r92integDates = []struct {
	in   string
	want time.Time
}{
	{"2026-01-02", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	{"2026-01-02T03:04:05.123456Z", time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)},
	{"Fri, 02 Jan 2026 03:04:05 GMT", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
}

func TestR92integInteractionsCreatedIsJSDate(t *testing.T) {
	for _, tc := range r92integDates {
		// generate (google-interactions-language-model.ts 589-593)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","created":%q,"steps":[{"type":"model_output","content":[{"type":"text","text":"hi"}]}]}`, tc.in)
		}))
		model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
		if err != nil {
			t.Fatal(err)
		}
		res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}}})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Errorf("generate created %q: timestamp = %+v, want %v", tc.in, res.ResponseMetadata, tc.want)
		}

		// stream (build-google-interactions-stream-transform.ts 218-226)
		body := `data: {"event_type":"interaction.created","interaction":{"id":"v1","created":"` + tc.in + `","model":"gemini-2.5-flash"}}` + "\n\n"
		s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
		var got *time.Time
		for _, c := range drainInteractions(t, s) {
			if c.Type == provider.ChunkTypeResponseMetadata {
				ts := c.ResponseMetadata.Timestamp
				got = &ts
			}
		}
		if got == nil || !got.Equal(tc.want) {
			t.Errorf("stream created %q: timestamp = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TS google-realtime-model.ts 143-145:
// Math.floor(new Date(expireTime).getTime() / 1000).
func TestR92integRealtimeExpireTimeIsJSDate(t *testing.T) {
	for in, want := range map[string]int64{
		"2026-01-02":                  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Unix(),
		"2026-01-02T03:04:05.999999Z": time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix(),
		"1969-12-31T23:59:59.5Z":      -1,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, `{"name":"tok","expireTime":%q}`, in)
		}))
		m, err := New(Config{APIKey: "k", BaseURL: server.URL + "/v1beta"}).RealtimeModel("gemini-live-2.5-flash-preview")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.(*GoogleRealtimeModel).DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.ExpiresAt == nil || *res.ExpiresAt != want {
			t.Errorf("expireTime %q: expiresAt = %v, want %d", in, res.ExpiresAt, want)
		}
	}
}
