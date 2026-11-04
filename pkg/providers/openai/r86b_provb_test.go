package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86bprovbRealtimeBearerTokenJSRegexp pins the realtime WebSocket auth
// to TS getOpenAIRealtimeConnection's /^bearer\s+(.+)$/i (V061 item 25): JS \s
// includes U+00A0, U+2003, U+FEFF and \v; JS . excludes U+2028 and \r. Each
// want was printed by Node 24.
func TestR86bprovbRealtimeBearerTokenJSRegexp(t *testing.T) {
	for _, tc := range []struct {
		value, token string
		ok           bool
	}{
		{"Bearer tok", "tok", true},
		{"bearer\ttok", "tok", true},
		{"Bearer\u00a0tok", "tok", true},
		{"Bearer\u2003tok", "tok", true},
		{"Bearer\u000btok", "tok", true},
		{"Bearer\ufefftok", "tok", true},
		{"Bearer\u0085tok", "", false},
		{"Bearer tok\u2028x", "", false},
		{"Bearer tok\rx", "", false},
		{"Bearer  ", " ", true},
		{"Bearer \u2028", "", false},
		{"Bearer", "", false},
		{"Bearer ", "", false},
		{"BEARER  a b ", "a b ", true},
		{"Bearer \u00a0 tok", "tok", true},
	} {
		protocols, headers := openAIRealtimeWSAuth(map[string]string{"Authorization": tc.value, "X": "y"})
		if !tc.ok {
			if len(protocols) != 1 || headers["Authorization"] != tc.value {
				t.Errorf("%q: protocols %q, headers %v; want no token, header kept", tc.value, protocols, headers)
			}
			continue
		}
		if len(protocols) != 2 || protocols[1] != "openai-insecure-api-key."+tc.token {
			t.Errorf("%q: protocols %q, want token %q", tc.value, protocols, tc.token)
		}
		if _, kept := headers["Authorization"]; kept || headers["X"] != "y" {
			t.Errorf("%q: headers %v", tc.value, headers)
		}
	}
}

// TestR86bprovbCompatProviderOptionsNameJSTrim pins TS
// providerOptionsName = provider.split('.')[0].trim() (JS trim: U+FEFF is
// trimmed, U+0085 is not) on the OpenAI-compatible chat and embedding paths;
// the embedding path did not trim at all.
func TestR86bprovbCompatProviderOptionsNameJSTrim(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	emb := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", Name: " deepinfra .embedding", OpenAICompatible: true}), "e")
	_, _ = emb.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"deepinfra": map[string]interface{}{"dimensions": float64(512)}},
	})
	if body["dimensions"] != float64(512) {
		t.Fatalf("embedding body = %#v, want dimensions 512 from providerOptions.deepinfra", body)
	}
	body = nil
	if _, err := emb.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"deepinfra": map[string]interface{}{"dimensions": "x"}},
	}); err == nil || body != nil {
		t.Fatalf("invalid providerOptions.deepinfra.dimensions: err %v, body %#v; want a validation error before the request", err, body)
	}

	for _, tc := range []struct {
		name, key string
		want      bool
	}{
		{"\ufeffacme.chat", "acme", true},
		{"acme\u0085.chat", "acme", false},
		{"acme\u0085.chat", "acme\u0085", true},
	} {
		chat := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", Name: tc.name, OpenAICompatible: true}), "m")
		_, _ = chat.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{tc.key: map[string]interface{}{"user": "u1"}},
		})
		if got := body["user"] == "u1"; got != tc.want {
			t.Errorf("provider %q, options key %q: user sent = %v, want %v (body %#v)", tc.name, tc.key, got, tc.want, body)
		}
	}
}

// TestR86bprovbBatchResultsBlankLinesJSTrim pins TS parseJsonLines
// (`line.trim().length > 0`, then parseJSON of the untrimmed row): a row of
// U+FEFF / U+3000 is skipped, a row of U+0085 and JSON behind U+00A0 fail.
func TestR86bprovbBatchResultsBlankLinesJSTrim(t *testing.T) {
	ok1 := `{"custom_id":"a","response":null,"error":{"code":"batch_cancelled","message":"x"}}`
	ok2 := `{"custom_id":"b","response":null,"error":{"code":"batch_cancelled","message":"x"}}`
	for _, tc := range []struct {
		name, content string
		items         int
		wantErr       bool
	}{
		{"blank FEFF and U+3000 rows", ok1 + "\n\ufeff\n\u3000\t\r\n" + ok2 + "\n", 2, false},
		{"U+0085 row", ok1 + "\n\u0085\n" + ok2 + "\n", 1, true},
		{"JSON behind U+00A0", ok1 + "\n\u00a0" + ok2 + "\n", 1, true},
	} {
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		mux.HandleFunc("/batches/batch_1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"batch_1","status":"completed","output_file_id":"file-out"}`))
		})
		mux.HandleFunc("/files/file-out/content", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.content))
		})
		stream, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch().DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_1"})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		items := 0
		var gotErr error
		for {
			_, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				gotErr = err
				break
			}
			items++
		}
		_ = stream.Close()
		srv.Close()
		if items != tc.items || (gotErr != nil) != tc.wantErr {
			t.Errorf("%s: %d items, err %v; want %d items, error %v", tc.name, items, gotErr, tc.items, tc.wantErr)
		}
	}
}
