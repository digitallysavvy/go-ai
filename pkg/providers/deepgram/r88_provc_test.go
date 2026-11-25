package deepgram

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS deepgram-speech-model.test.ts (5cc6dcaa65).
func TestR88provcDeepgramSpeechCallback(t *testing.T) {
	for _, opts := range []map[string]interface{}{
		{"callback": "https://example.com/callback"},
		{"callback": "https://example.com/callback", "callbackMethod": "PUT"},
	} {
		query := "unset"
		srv := speechTestServer(t, nil, &query)
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2")
		_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text: "Hello, welcome to Deepgram!", Voice: "helena",
			ProviderOptions: map[string]interface{}{"deepgram": opts},
		})
		srv.Close()
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions.deepgram.callback" || !strings.Contains(err.Error(), "returns a JSON acknowledgement instead of audio") {
			t.Fatalf("%v: err = %v", opts, err)
		}
		if query != "unset" {
			t.Fatalf("%v: a request was sent (%q)", opts, query)
		}
	}

	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2")
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, welcome to Deepgram!", Voice: "helena",
		ProviderOptions: map[string]interface{}{"deepgram": map[string]interface{}{"callbackMethod": "PUT"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "callback") {
		t.Fatalf("query = %s", query)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "providerOptions.deepgram.callbackMethod", Details: "providerOptions.deepgram.callbackMethod is only used together with `callback`, which is not supported by the AI SDK. Callback method was ignored."}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
}
