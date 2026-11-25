package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from openai-transcription-model.test.ts "detected languages"
// (TS #23589).
func TestTranscriptionDetectedLanguages(t *testing.T) {
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b := body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b))
	}))
	defer srv.Close()
	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-transcribe")

	cases := []struct {
		name     string
		fields   string
		expected string
		hasMeta  bool
		meta     interface{}
	}{
		{"english name", `"language":"english"`, "en", false, nil},
		{"code en", `"language":"en"`, "en", false, nil},
		{"code bn", `"language":"bn"`, "bn", false, nil},
		{"unknown", `"language":"unknown"`, "", false, nil},
		{"region code", `"language":"en-US"`, "", false, nil},
		{"constructor", `"language":"constructor"`, "", false, nil},
		{"single en", `"languages":[{"code":"en"}]`, "en", true, []interface{}{map[string]interface{}{"code": "en"}}},
		{"single fr", `"languages":[{"code":"fr"}]`, "fr", true, []interface{}{map[string]interface{}{"code": "fr"}}},
		{"multiple", `"languages":[{"code":"en"},{"code":"fr"}]`, "", true, []interface{}{map[string]interface{}{"code": "en"}, map[string]interface{}{"code": "fr"}}},
		{"region in languages", `"languages":[{"code":"en-US"}]`, "", true, []interface{}{map[string]interface{}{"code": "en-US"}}},
		{"name in languages", `"languages":[{"code":"english"}]`, "", true, []interface{}{map[string]interface{}{"code": "english"}}},
		{"empty languages", `"languages":[]`, "", true, []interface{}{}},
		{"both null", `"language":null,"languages":null`, "", false, nil},
		{"unknown + languages", `"language":"unknown","languages":[{"code":"en"}]`, "en", true, []interface{}{map[string]interface{}{"code": "en"}}},
		{"french name + languages", `"language":"french","languages":[{"code":"en"}]`, "fr", true, []interface{}{map[string]interface{}{"code": "en"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			body = `{"text":"Hello world",` + tc.fields + `}`
			mu.Unlock()
			res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
				Audio: []byte("audio"), MimeType: "audio/mpeg",
				ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"responseFormat": "json", "timestampGranularities": []interface{}{}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Language != tc.expected {
				t.Errorf("language = %q, want %q", res.Language, tc.expected)
			}
			if !tc.hasMeta {
				if res.ProviderMetadata != nil {
					t.Errorf("providerMetadata = %#v, want nil", res.ProviderMetadata)
				}
				return
			}
			raw, _ := json.Marshal(res.ProviderMetadata["openai"].(map[string]interface{})["languages"])
			var got interface{}
			_ = json.Unmarshal(raw, &got)
			if !reflect.DeepEqual(got, tc.meta) {
				t.Errorf("languages = %s", raw)
			}
		})
	}

	t.Run("alongside diarized segments", func(t *testing.T) {
		mu.Lock()
		body = `{"text":"Hello world","languages":[{"code":"en"},{"code":"fr-CA"}],"segments":[{"type":"transcript.text.segment","id":"segment-0","start":0,"end":1,"text":"Hello world","speaker":"A"}]}`
		mu.Unlock()
		res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("audio"), MimeType: "audio/mpeg"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Language != "" {
			t.Errorf("language = %q", res.Language)
		}
		meta := res.ProviderMetadata["openai"].(map[string]interface{})
		if meta["segments"] == nil || meta["languages"] == nil {
			t.Fatalf("metadata = %#v", meta)
		}
	})
}
