package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs sends the call options as given (minus abortSignal), so a
// caller's empty headers map is in the body as {}.
func TestR94provgEmptyHeadersForwarded(t *testing.T) {
	m := newTestGatewayLanguageModel("openai/gpt-5")
	body, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := body["headers"].(map[string]string); !ok || len(h) != 0 {
		t.Fatalf("headers = %#v (present %v)", body["headers"], ok)
	}
	body, _ = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["headers"]; ok {
		t.Fatal("nil headers should be omitted")
	}
}

func r94provgDecide(t *testing.T, response string, po map[string]interface{}) (*provider.DecisionResult, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.DecisionModel("typesafe-ai/jev")
	if err != nil {
		t.Fatal(err)
	}
	return model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "s", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}, ProviderOptions: po,
	})
}

// TS `responseBody.model ?? this.modelId`: a present empty model is kept.
func TestR94provgDecisionEmptyModelKept(t *testing.T) {
	res, err := r94provgDecide(t, `{"answers":{},"model":""}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.ModelID != "" {
		t.Fatalf("modelId = %q, want the response's empty model", res.Response.ModelID)
	}
}

// gatewayDecisionAnswerSchema and gatewayDecisionWarningSchema are
// discriminated unions: each member's zod object drops the keys only another
// member has.
func TestR94provgDecisionAnswerAndWarningKeysStripped(t *testing.T) {
	res, err := r94provgDecide(t, `{"answers":{
		"b":{"type":"boolean","probability":0.5,"choice":"x","score":2,"probabilities":{"x":1}},
		"c":{"type":"choice","choice":"x","probability":0.5,"score":2},
		"s":{"type":"score","score":2,"choice":"x","probability":0.5},
		"r":{"type":"refusal","choice":"x","probability":0.5}},
		"warnings":[
		{"type":"unsupported","feature":"f","details":"d","message":"m","setting":"s"},
		{"type":"deprecated","setting":"s","message":"m","feature":"f","details":"d"},
		{"type":"other","message":"m","feature":"f","details":"d","setting":"s"}]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	half, two := 0.5, 2.0
	want := map[string]provider.DecisionAnswer{
		"b": {Type: "boolean", Probability: &half},
		"c": {Type: "choice", Choice: "x"},
		"s": {Type: "score", Score: &two},
		"r": {Type: "refusal"},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %#v", res.Answers)
	}
	wantWarnings := []types.Warning{
		{Type: "unsupported", Feature: "f", Details: "d"},
		{Type: "deprecated", Setting: "s", Message: "m"},
		{Type: "other", Message: "m"},
	}
	if !reflect.DeepEqual(res.Warnings, wantWarnings) {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
}

// The other gateway models share the warning schema.
func TestR94provgWarningSchemaSweep(t *testing.T) {
	in := []types.Warning{{Type: "compatibility", Feature: "f", Details: "d", Message: "m"}}
	want := []types.Warning{{Type: "compatibility", Feature: "f", Details: "d"}}
	if got := warningsOrEmpty(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("warningsOrEmpty = %#v", got)
	}
	if got := convertWarningData([]warningData{{Type: "compatibility", Feature: "f", Details: "d", Message: "m"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("convertWarningData = %#v", got)
	}
	if got := warningsOrEmpty(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil warnings = %#v, want []", got)
	}
}

// TS parseProviderOptions with z.catchall(z.object(...)): a gateway
// namespace that is not an object fails the call before any request; null
// is no options.
func TestR94provgDecisionNonObjectGatewayOptions(t *testing.T) {
	_, err := r94provgDecide(t, `{"answers":{}}`, map[string]interface{}{"gateway": "x"})
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) || argErr.Message != "invalid gateway provider options" {
		t.Fatalf("err = %T %v", err, err)
	}
	if _, err := r94provgDecide(t, `{"answers":{}}`, map[string]interface{}{"gateway": nil}); err != nil {
		t.Fatalf("null namespace: %v", err)
	}
}

// The metadata and credits fetches are TS getFromApi (`!response.ok` runs
// the failed-response handler): an unfollowed 3xx is a Gateway error, not a
// response to parse.
func TestR94provgMetadataAndOriginNon2xxFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"models":[],"balance":"1","total_used":"0","error":{"message":"moved","type":"x"}}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v3/ai"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetAvailableModels(t.Context()); err == nil {
		t.Error("metadata: 300 accepted")
	}
	if _, err := p.GetCredits(t.Context()); err == nil {
		t.Error("credits: 300 accepted")
	}
}

// cr-r94-carry-to-provg class 1: TS gateway-speech-model sends every client
// frame with JSON.stringify, so <, > and & stay literal (Go's json.Marshal
// writes <, >, &).
func TestR94provgSpeechFramesAreJSONStringifyText(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	result, err := model.DoStreamSpeech(t.Context(), &provider.SpeechStreamOptions{
		Voice: "a<b",
		Text:  newSpeechTestText(provider.SpeechStreamTextPart{Text: "a<b&c"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.text-done", 5*time.Second)
	server.mu.Lock()
	text := append([]string(nil), server.receivedText...)
	server.mu.Unlock()
	if len(text) < 2 || !strings.Contains(text[0], `"a<b"`) || text[1] != `{"type":"speech-stream.text-delta","delta":"a<b&c"}` {
		t.Fatalf("frames = %q", text)
	}
}

// cr-r94-carry-to-provg class 1 sweep: the transcription start frame, the
// realtime client events and the client-secret body are JSON.stringify text
// in TS.
func TestR94provgJSONStringifySweep(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	tm := newTestGatewayTranscriptionModel(t, server.ts.URL)
	res, err := tm.DoStream(t.Context(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		ProviderOptions:  map[string]interface{}{"x": map[string]interface{}{"y": "a<b&c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	deadline := time.Now().Add(5 * time.Second)
	var first string
	for time.Now().Before(deadline) && first == "" {
		server.mu.Lock()
		if len(server.receivedText) > 0 {
			first = server.receivedText[0]
		}
		server.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(first, `"a<b&c"`) {
		t.Errorf("transcription start frame = %q", first)
	}

	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	instr := "a<b&c"
	raw, err := p.ExperimentalRealtime("m").SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-update", Config: provider.RealtimeSessionConfig{Instructions: &instr}})
	if err != nil || !strings.Contains(string(raw), "a<b&c") {
		t.Errorf("realtime event = %s, %v", raw, err)
	}

	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		body = b.String()
		_, _ = w.Write([]byte(`{"token":"t"}`))
	}))
	defer srv.Close()
	p2, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v3/ai"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p2.MintRealtimeClientSecret(t.Context(), MintRealtimeClientSecretParams{ModelID: "a<b&c"})
	if !strings.Contains(body, `"a<b&c"`) {
		t.Errorf("client secret body = %q", body)
	}
}

// cr-r94-prova-to-provg 2: TS connectToWebSocket fails the stream with
// abortSignal.reason, so the caller's cancel cause is the stream error.
func TestR94provgAbortCause(t *testing.T) {
	reason := errors.New("r94provg user stop")
	drain := func(name string, next func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			for {
				if err := next(); err != nil {
					done <- err
					return
				}
			}
		}()
		select {
		case err := <-done:
			if !errors.Is(err, reason) {
				t.Errorf("%s: err = %v, want the cancel cause", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: no error after cancel", name)
		}
	}

	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newTestGatewayTranscriptionModel(t, server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	cancel(reason)
	drain("transcription", func() error { _, err := res.Stream.Next(); return err })

	server2 := newGatewayTranscriptionTestServer(t)
	defer server2.close()
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	sres, err := newTestGatewaySpeechModel(t, server2.ts.URL).DoStreamSpeech(ctx2, &provider.SpeechStreamOptions{Text: newSpeechTestText()})
	if err != nil {
		t.Fatal(err)
	}
	defer sres.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	cancel2(reason)
	drain("speech", func() error { _, err := sres.Stream.Next(); return err })
}
