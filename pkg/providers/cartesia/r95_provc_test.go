package cartesia

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doCreateClientSecret sends JSON.stringify({grants: {stt: true},
// ...{expires_in}}): grants comes first on the wire.
func TestR95provcRealtimeClientSecretBodyOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"token":"access-token"}`))
	}))
	defer srv.Close()
	m := newTestRealtimeModel(srv.URL)
	if _, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: intp(60)}); err != nil {
		t.Fatal(err)
	}
	if want := `{"grants":{"stt":true},"expires_in":60}`; body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}
