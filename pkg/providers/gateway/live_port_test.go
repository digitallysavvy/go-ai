package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const port2provgLiveURL = "wss://ai-gateway.vercel.sh/v4/ai/live-model?ai-model-id=openai%2Fgpt-live-1.1"

func port2provgLiveProvider(t *testing.T, baseURL, team string) *Provider {
	t.Helper()
	if baseURL == "" {
		baseURL = "https://ai-gateway.vercel.sh/v4/ai"
	}
	p, err := New(Config{APIKey: "vck_test-token", BaseURL: baseURL, TeamIDOrSlug: team})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func port2provgLiveModel(t *testing.T) *RealtimeModelLive {
	return port2provgLiveProvider(t, "", "").ExperimentalLive("openai/gpt-live-1.1")
}

func port2provgStr(s string) *string { return &s }

func port2provgIsUnsupported(err error) bool {
	var u *providererrors.UnsupportedFunctionalityError
	return errors.As(err, &u)
}

func port2provgIsInvalidArg(err error) bool {
	var a *providererrors.InvalidArgumentError
	return errors.As(err, &a)
}

// TS: 'declares continuous Live capabilities'
func TestPortprovgLiveCapabilities(t *testing.T) {
	m := port2provgLiveModel(t)
	if m.SpecificationVersion() != "v4" || m.Provider() != "gateway.live" || m.ModelID() != "openai/gpt-live-1.1" {
		t.Fatal("metadata")
	}
	if m.RealtimeConversation() != "continuous" {
		t.Fatal("conversation")
	}
	if !reflect.DeepEqual(m.RealtimeConnections(), []string{"client-secret-websocket", "server-websocket"}) {
		t.Fatal("connections")
	}
	if lc := m.RealtimeLifecycle(); lc.StartupEventType != "session-start" || lc.FinalizationEventType != "session-close" {
		t.Fatalf("lifecycle %+v", lc)
	}
	// The server branch would shadow the client-secret branch.
	var model provider.Experimental_RealtimeModelV4 = m
	if _, ok := model.(provider.RealtimeServerWebSocketConfigProvider); ok {
		t.Fatal("must not implement RealtimeServerWebSocketConfigProvider")
	}
}

// TS: 'mints a Live-bound secret and returns the live-model url' and
// 'upgrades an http base url to ws'
func TestPortprovgLiveCreateClientSecret(t *testing.T) {
	var path string
	var raw []byte
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_live","expiresAt":1700000060}`))
	}))
	defer closeServer()
	p := port2provgLiveProvider(t, serverURL+"/v4/ai", "")
	expires := 120
	res, err := p.ExperimentalLive("openai/gpt-live-1.1").DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/realtime/client-secrets" {
		t.Fatalf("path %q", path)
	}
	if string(raw) != `{"model":"openai/gpt-live-1.1","routeKind":"live","expiresIn":120}` {
		t.Fatalf("body %s", raw)
	}
	wantURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/v4/ai/live-model?ai-model-id=openai%2Fgpt-live-1.1"
	if res.Token != "vcst_live" || res.URL != wantURL || res.ExpiresAt == nil || *res.ExpiresAt != 1700000060 {
		t.Fatalf("result %+v want url %s", res, wantURL)
	}
}

// TS: 'carries the token and team scope in WebSocket subprotocols'
func TestPortprovgLiveWebSocketConfig(t *testing.T) {
	m := port2provgLiveProvider(t, "", "my-team").ExperimentalLive("openai/gpt-live-1.1")
	cfg := m.GetWebSocketConfig("vcst_x", port2provgLiveURL)
	if cfg.URL != port2provgLiveURL || cfg.Protocols[0] != GatewayRealtimeSubprotocol || cfg.Protocols[1] != GatewayAuthSubprotocolPrefix+"vcst_x" {
		t.Fatalf("cfg %+v", cfg)
	}
	if got := GetGatewayRealtimeTeamIDOrSlug(strings.Join(cfg.Protocols, ", ")); got != "my-team" {
		t.Fatalf("team %q", got)
	}
}

// TS: 'returns the live-model url and defined headers for server connections'
func TestPortprovgLiveServerConnection(t *testing.T) {
	cfg, err := port2provgLiveModel(t).GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != port2provgLiveURL || cfg.Headers["authorization"] != "Bearer vck_test-token" {
		t.Fatalf("cfg %+v", cfg)
	}
	for k, v := range cfg.Headers {
		if v == "" {
			t.Fatalf("empty header %s", k)
		}
	}
	if cfg.Protocols != nil {
		t.Fatalf("protocols %v", cfg.Protocols)
	}
}

// TS: 'rejects non-conversation intents'
func TestPortprovgLiveRejectsNonConversationIntents(t *testing.T) {
	m := port2provgLiveModel(t)
	const want = "does not support the 'transcription' session intent"
	if _, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: provider.RealtimeSessionIntentTranscription}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err %v", err)
	}
	if _, err := m.SerializeClientEventForIntent(provider.RealtimeClientEvent{Type: "session-close"}, provider.RealtimeSessionIntentTranscription); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("err %v", err)
	}
	if _, err := m.BuildSessionConfigForIntent(provider.RealtimeSessionConfig{}, provider.RealtimeSessionIntentTranscription); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("err %v", err)
	}
	// The conversation intent works.
	if _, err := m.BuildSessionConfigForIntent(provider.RealtimeSessionConfig{}, provider.RealtimeSessionIntentConversation); err != nil {
		t.Fatal(err)
	}
}

// TS: 'adds the model id to session-start'
func TestPortprovgLiveSessionStartAddsModelID(t *testing.T) {
	rate := 24000
	raw, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{
		Type: "session-start", EventID: "start-1",
		Config: provider.RealtimeSessionConfig{
			Instructions:      port2provgStr("Be brief."),
			Voice:             port2provgStr("marin"),
			InputAudioFormat:  &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &rate},
			OutputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &rate},
			ProviderOptions: map[string]interface{}{
				"gateway": map[string]interface{}{"tags": []interface{}{"demo"}},
				"openai":  map[string]interface{}{"delegation": map[string]interface{}{"type": "client"}},
			},
		},
	})
	want := `{"type":"session-start","modelId":"openai/gpt-live-1.1","config":{"instructions":"Be brief.","voice":"marin","inputAudioFormat":{"type":"audio/pcm","rate":24000},"outputAudioFormat":{"type":"audio/pcm","rate":24000},"providerOptions":{"gateway":{"tags":["demo"]},"openai":{"delegation":{"type":"client"}}}},"eventId":"start-1"}`
	if err != nil || string(raw) != want {
		t.Fatalf("err %v\n got %s\nwant %s", err, raw, want)
	}
}

// TS: 'maps %o to the normalized Live event' (7 rows)
func TestPortprovgLiveNormalizedClientEvents(t *testing.T) {
	d1 := "d1"
	cases := []struct {
		event provider.RealtimeClientEvent
		want  string
	}{
		{provider.RealtimeClientEvent{Type: "session-close"}, `{"type":"session-close"}`},
		{provider.RealtimeClientEvent{Type: "session-close", EventID: "close-1"}, `{"type":"session-close","eventId":"close-1"}`},
		{provider.RealtimeClientEvent{Type: "input-audio-mute"}, `{"type":"input-audio-mute"}`},
		{provider.RealtimeClientEvent{Type: "input-audio-unmute"}, `{"type":"input-audio-unmute"}`},
		{provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "AAAA"}, `{"type":"input-audio-append","audio":"AAAA"}`},
		{provider.RealtimeClientEvent{Type: "context-append", Content: "Note"}, `{"type":"context-append","content":"Note","delegationId":null}`},
		{provider.RealtimeClientEvent{Type: "context-append", Content: "Checking", DelegationID: &d1, EventID: "ctx-1", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"channel": "thinking"}}},
			`{"type":"context-append","content":"Checking","delegationId":"d1","providerOptions":{"openai":{"channel":"thinking"}},"eventId":"ctx-1"}`},
	}
	for _, tc := range cases {
		raw, err := port2provgLiveModel(t).SerializeClientEvent(tc.event)
		if err != nil || string(raw) != tc.want {
			t.Errorf("%s: err %v got %s want %s", tc.event.Type, err, raw, tc.want)
		}
	}
}

// TS: 'rejects context-append provider options %o' (4 rows)
func TestPortprovgLiveRejectsContextProviderOptions(t *testing.T) {
	cases := []map[string]interface{}{
		{"openai": map[string]interface{}{"channel": "shouting"}},
		{"openai": map[string]interface{}{"channel": nil}},
		{"openai": map[string]interface{}{"priority": "high"}},
		{"google": map[string]interface{}{"channel": "thinking"}},
	}
	for _, po := range cases {
		_, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{Type: "context-append", Content: "Note", ProviderOptions: po})
		if !port2provgIsUnsupported(err) {
			t.Errorf("%v: err %v", po, err)
		}
	}
}

// TS: 'accepts the %s context channel' (3 rows)
func TestPortprovgLiveAcceptsContextChannels(t *testing.T) {
	for _, channel := range []string{"commentary", "instructions", "thinking"} {
		raw, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{Type: "context-append", Content: "Note",
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"channel": channel}}})
		if err != nil || !strings.Contains(string(raw), `"providerOptions":{"openai":{"channel":"`+channel+`"}}`) {
			t.Errorf("%s: %v %s", channel, err, raw)
		}
	}
}

// TS: 'rejects $type, which Live does not accept' (5 rows)
func TestPortprovgLiveRejectsUnsupportedClientEvents(t *testing.T) {
	for _, typ := range []string{"session-update", "input-audio-commit", "input-audio-clear", "response-create", "response-cancel"} {
		_, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{Type: typ})
		if !port2provgIsUnsupported(err) || !strings.Contains(err.Error(), "AI Gateway Live client event: "+typ) {
			t.Errorf("%s: %v", typ, err)
		}
	}
}

// TS: 'rejects the unsupported %s setting' (3 rows)
func TestPortprovgLiveRejectsUnsupportedSessionSettings(t *testing.T) {
	cases := map[string]provider.RealtimeSessionConfig{
		"tools":            {Tools: []provider.RealtimeToolDefinition{}},
		"turnDetection":    {TurnDetection: &provider.RealtimeTurnDetection{Type: "server-vad"}},
		"outputModalities": {OutputModalities: []string{}},
	}
	for key, cfg := range cases {
		_, err := port2provgLiveModel(t).BuildSessionConfigForIntent(cfg, "")
		if !port2provgIsUnsupported(err) || !strings.Contains(err.Error(), "AI Gateway Live session setting: "+key) {
			t.Errorf("%s: %v", key, err)
		}
		// session-start serialization raises it too.
		if _, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-start", Config: cfg}); !port2provgIsUnsupported(err) {
			t.Errorf("%s serialize: %v", key, err)
		}
	}
}

func port2provgBuild(t *testing.T, po map[string]interface{}) error {
	t.Helper()
	_, err := port2provgLiveModel(t).BuildSessionConfigForIntent(provider.RealtimeSessionConfig{ProviderOptions: po}, "")
	return err
}

// TS: 'rejects the provider options %o' (8 rows; a JS null is a Go nil)
func TestPortprovgLiveRejectsProviderOptions(t *testing.T) {
	type row struct {
		po          map[string]interface{}
		unsupported bool
	}
	rows := []row{
		{map[string]interface{}{"openai": map[string]interface{}{"client": map[string]interface{}{"dataChannel": map[string]interface{}{}}}}, true},
		{map[string]interface{}{"openai": map[string]interface{}{"delegation": map[string]interface{}{"type": "responses"}}}, true},
		{map[string]interface{}{"openai": map[string]interface{}{"delegation": "client"}}, true},
		{map[string]interface{}{"openai": map[string]interface{}{"voice": nil}}, false},
		{map[string]interface{}{"openai": map[string]interface{}{"store": nil}}, false},
		{map[string]interface{}{"openai": nil}, false},
		{map[string]interface{}{"gateway": "tags"}, false},
		{map[string]interface{}{"google": map[string]interface{}{}}, true},
	}
	for i, r := range rows {
		err := port2provgBuild(t, r.po)
		if err == nil || (r.unsupported && !port2provgIsUnsupported(err)) || (!r.unsupported && !port2provgIsInvalidArg(err)) {
			t.Errorf("row %d %v: err %v", i, r.po, err)
		}
	}
}

func port2provgMessage(role, typ string) map[string]interface{} {
	return map[string]interface{}{"type": "message", "role": role, "content": []interface{}{map[string]interface{}{"type": typ, "text": "Hi"}}}
}

// TS: 'rejects the malformed openai.%s value %o' (12 rows)
func TestPortprovgLiveRejectsMalformedOpenAIOptions(t *testing.T) {
	extra := port2provgMessage("user", "input_text")
	extra["extra"] = true
	many := make([]interface{}, 129)
	for i := range many {
		many[i] = port2provgMessage("user", "input_text")
	}
	rows := []struct {
		key   string
		value interface{}
	}{
		{"voice", "marin"},
		{"voice", map[string]interface{}{"id": ""}},
		{"voice", map[string]interface{}{"id": "v", "name": "extra"}},
		{"store", "false"},
		{"input", map[string]interface{}{}},
		{"input", []interface{}{nil}}, // the JS hole (new Array(1)) reads as undefined
		{"input", []interface{}{port2provgMessage("user", "text")}},
		{"input", []interface{}{port2provgMessage("assistant", "input_text")}},
		{"input", []interface{}{port2provgMessage("system", "input_text")}},
		{"input", []interface{}{extra}},
		{"input", []interface{}{map[string]interface{}{"type": "message", "role": "user", "content": []interface{}{}}}},
		{"input", many},
	}
	for i, r := range rows {
		err := port2provgBuild(t, map[string]interface{}{"openai": map[string]interface{}{r.key: r.value}})
		if !port2provgIsInvalidArg(err) {
			t.Errorf("row %d openai.%s: err %v", i, r.key, err)
		} else {
			var arg *providererrors.InvalidArgumentError
			errors.As(err, &arg)
			if arg.Field != "providerOptions.openai."+r.key {
				t.Errorf("row %d: argument %q", i, arg.Field)
			}
		}
	}
}

// TS: 'rejects delegation objects with extra keys'
func TestPortprovgLiveRejectsDelegationWithExtraKeys(t *testing.T) {
	err := port2provgBuild(t, map[string]interface{}{"openai": map[string]interface{}{"delegation": map[string]interface{}{"type": "client", "extra": true}}})
	if !port2provgIsUnsupported(err) {
		t.Fatalf("err %v", err)
	}
}

// TS: 'accepts 128 input messages and every supported role and part type'
func TestPortprovgLiveAccepts128InputMessages(t *testing.T) {
	input := []interface{}{
		port2provgMessage("developer", "input_text"), port2provgMessage("user", "input_text"),
		port2provgMessage("assistant", "text"), port2provgMessage("assistant", "output_text"),
	}
	for i := 0; i < 124; i++ {
		input = append(input, port2provgMessage("user", "input_text"))
	}
	if err := port2provgBuild(t, map[string]interface{}{"openai": map[string]interface{}{"input": input}}); err != nil {
		t.Fatal(err)
	}
	// Typed Go containers validate like the JS objects.
	typed := []map[string]interface{}{port2provgMessage("user", "input_text")}
	if err := port2provgBuild(t, map[string]interface{}{"openai": map[string]interface{}{"input": typed}}); err != nil {
		t.Fatal(err)
	}
}

// TS: 'accepts supported provider options and leaves gateway options open'
// (the JS `unset: undefined` member has no Go value: the key is left out).
func TestPortprovgLiveAcceptsSupportedProviderOptions(t *testing.T) {
	po := map[string]interface{}{
		"gateway": map[string]interface{}{"tags": []interface{}{"demo"}, "futureOption": map[string]interface{}{"nested": true}},
		"openai": map[string]interface{}{
			"delegation": nil,
			"input":      []interface{}{port2provgMessage("user", "input_text")},
			"store":      false,
			"voice":      map[string]interface{}{"id": "voice_1"},
		},
	}
	got, err := port2provgLiveModel(t).BuildSessionConfigForIntent(provider.RealtimeSessionConfig{ProviderOptions: po}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, provider.RealtimeSessionConfig{ProviderOptions: po}) {
		t.Fatalf("got %+v", got)
	}
}

// TS: 'ignores unset settings'
func TestPortprovgLiveIgnoresUnsetSettings(t *testing.T) {
	raw, err := port2provgLiveModel(t).SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-start", Config: provider.RealtimeSessionConfig{Instructions: port2provgStr("Hi")}})
	if err != nil || string(raw) != `{"type":"session-start","modelId":"openai/gpt-live-1.1","config":{"instructions":"Hi"}}` {
		t.Fatalf("%v %s", err, raw)
	}
}

// TS: 'rejects the unsupported audio format %o' (4 rows)
func TestPortprovgLiveRejectsUnsupportedAudioFormats(t *testing.T) {
	r48, r16 := 48000, 16000
	r24 := 24000
	rows := []struct {
		f    provider.RealtimeAudioFormat
		text string
	}{
		{provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &r48}, "audio/pcm at 48000 Hz"},
		{provider.RealtimeAudioFormat{Type: "audio/pcm"}, "audio/pcm at an unspecified Hz"},
		{provider.RealtimeAudioFormat{Type: "audio/pcmu", Rate: &r16}, "audio/pcmu at 16000 Hz"},
		{provider.RealtimeAudioFormat{Type: "audio/opus", Rate: &r24}, "audio/opus at 24000 Hz"},
	}
	for _, r := range rows {
		f := r.f
		_, err := port2provgLiveModel(t).BuildSessionConfigForIntent(provider.RealtimeSessionConfig{InputAudioFormat: &f}, "")
		if !port2provgIsUnsupported(err) || !strings.Contains(err.Error(), "AI Gateway Live inputAudioFormat "+r.text+"; use audio/pcm at 16000 or 24000 Hz, or audio/pcma or audio/pcmu at 8000 Hz") {
			t.Errorf("%v: %v", r.f, err)
		}
	}
	// The output format is checked with its own argument name.
	f := provider.RealtimeAudioFormat{Type: "audio/opus", Rate: &r24}
	if _, err := port2provgLiveModel(t).BuildSessionConfigForIntent(provider.RealtimeSessionConfig{OutputAudioFormat: &f}, ""); err == nil || !strings.Contains(err.Error(), "AI Gateway Live outputAudioFormat audio/opus") {
		t.Fatalf("err %v", err)
	}
}

// TS: 'accepts the supported audio formats'
func TestPortprovgLiveAcceptsSupportedAudioFormats(t *testing.T) {
	r16, r24, r8 := 16000, 24000, 8000
	for _, f := range []provider.RealtimeAudioFormat{{Type: "audio/pcm", Rate: &r16}, {Type: "audio/pcm", Rate: &r24}, {Type: "audio/pcma", Rate: &r8}, {Type: "audio/pcmu", Rate: &r8}} {
		in, out := f, f
		cfg := provider.RealtimeSessionConfig{InputAudioFormat: &in, OutputAudioFormat: &out}
		got, err := port2provgLiveModel(t).BuildSessionConfigForIntent(cfg, "")
		if err != nil || !reflect.DeepEqual(got, cfg) {
			t.Errorf("%v: %v", f, err)
		}
	}
}

// TS: 'requires matching input and output formats'
func TestPortprovgLiveRequiresMatchingAudioFormats(t *testing.T) {
	r16, r24 := 16000, 24000
	cfg := provider.RealtimeSessionConfig{
		InputAudioFormat:  &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &r16},
		OutputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &r24},
	}
	_, err := port2provgLiveModel(t).BuildSessionConfigForIntent(cfg, "")
	if !port2provgIsInvalidArg(err) || err.Error() != "AI Gateway Live requires the same input and output audio format." {
		t.Fatalf("err %v", err)
	}
}

// TS: 'passes normalized events through'
func TestPortprovgLiveParsePassesNormalizedEventsThrough(t *testing.T) {
	events, err := port2provgLiveModel(t).ParseServerEvent(json.RawMessage(`{"type":"session-closed","sessionId":"live_1","usage":{"seconds":14},"reason":"close_requested","raw":{"type":"session.closed"}}`))
	if err != nil || len(events) != 1 {
		t.Fatal(err, events)
	}
	e := events[0]
	if e.Type != "session-closed" || e.SessionID != "live_1" || e.Reason != "close_requested" || e.Usage == nil || e.Usage.Seconds != 14 || string(e.Raw) != `{"type":"session.closed"}` {
		t.Fatalf("event %+v raw %s", e, e.Raw)
	}
}

// TS: 'keeps Gateway error frames as raw'
func TestPortprovgLiveParseKeepsGatewayErrorFramesAsRaw(t *testing.T) {
	frame := `{"type":"error","code":"invalid_request_error","message":"Live requires a canonical model slug"}`
	events, err := port2provgLiveModel(t).ParseServerEvent(json.RawMessage(frame))
	if err != nil || len(events) != 1 {
		t.Fatal(err)
	}
	e := events[0]
	if e.Type != "error" || e.Code != "invalid_request_error" || e.Message != "Live requires a canonical model slug" || string(e.Raw) != frame {
		t.Fatalf("event %+v raw %s", e, e.Raw)
	}
	// An error frame that carries its own raw member keeps it.
	events, _ = port2provgLiveModel(t).ParseServerEvent(json.RawMessage(`{"type":"error","message":"m","raw":{"p":1}}`))
	if string(events[0].Raw) != `{"p":1}` {
		t.Fatalf("raw %s", events[0].Raw)
	}
}

// TS: 'reports %o as an invalid server event' (5 rows)
func TestPortprovgLiveParseReportsInvalidServerEvents(t *testing.T) {
	for _, frame := range []string{`null`, `"text"`, `[]`, `{}`, `{"type":1}`} {
		events, err := port2provgLiveModel(t).ParseServerEvent(json.RawMessage(frame))
		if err != nil || len(events) != 1 {
			t.Fatalf("%s: %v", frame, err)
		}
		e := events[0]
		if e.Type != "error" || e.Code != "invalid_server_event" || e.Message != "Invalid AI Gateway Live server event." || string(e.Raw) != frame {
			t.Errorf("%s: %+v raw %s", frame, e, e.Raw)
		}
	}
}

// TS: 'creates a Live model for %s' (2), 'keeps %s on the realtime route' (3),
// 'honors an explicit api option', 'rejects an unknown api option'
func TestPortprovgLiveRouting(t *testing.T) {
	p := port2provgLiveProvider(t, "", "")
	for _, id := range []string{"openai/gpt-live-1", "openai/gpt-live-1.1"} {
		m, err := p.ExperimentalRealtimeModel(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m.(*RealtimeModelLive); !ok || m.Provider() != "gateway.live" {
			t.Errorf("%s: %T", id, m)
		}
	}
	for _, id := range []string{"openai/gpt-realtime-2", "google/gemini-3.8-live", "spacexai/grok-voice-3.0"} {
		m, err := p.ExperimentalRealtimeModel(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m.(*RealtimeModel); !ok || m.Provider() != "gateway.realtime" {
			t.Errorf("%s: %T", id, m)
		}
	}
	m, _ := p.ExperimentalRealtimeModel("openai/gpt-live-2", RealtimeModelOptions{API: port2provgStr("live")})
	if _, ok := m.(*RealtimeModelLive); !ok {
		t.Errorf("explicit live: %T", m)
	}
	m, _ = p.ExperimentalRealtimeModel("openai/gpt-live-1.1", RealtimeModelOptions{API: port2provgStr("realtime")})
	if _, ok := m.(*RealtimeModel); !ok {
		t.Errorf("explicit realtime: %T", m)
	}
	for _, bad := range []string{"other", ""} {
		_, err := p.ExperimentalRealtimeModel("openai/gpt-live-1.1", RealtimeModelOptions{API: port2provgStr(bad)})
		if !port2provgIsInvalidArg(err) || err.Error() != `AI Gateway realtime api must be "live" or "realtime".` {
			t.Errorf("api %q: %v", bad, err)
		}
	}
	// ExperimentalRealtime keeps returning the turn-based model.
	if p.ExperimentalRealtime("openai/gpt-live-1.1").Provider() != "gateway.realtime" {
		t.Error("ExperimentalRealtime changed")
	}
}

// TS: 'mints a Live token with routeKind live and returns the live-model url'
// and 'routes getToken by the api option and leaves realtime mints unchanged'
func TestPortprovgLiveGetToken(t *testing.T) {
	var bodies []string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_live","expiresAt":1700000060}`))
	}))
	defer closeServer()
	p := port2provgLiveProvider(t, serverURL+"/v4/ai", "")
	wsBase := "ws" + strings.TrimPrefix(serverURL, "http") + "/v4/ai"

	sixty := 60
	res, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-live-1.1", ClientSecretOptions: provider.ClientSecretOptions{ExpiresAfterSeconds: &sixty}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "vcst_live" || res.URL != wsBase+"/live-model?ai-model-id=openai%2Fgpt-live-1.1" || *res.ExpiresAt != 1700000060 {
		t.Fatalf("res %+v", res)
	}
	live, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-live-2", API: port2provgStr("live")})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-live-1.1", API: port2provgStr("realtime")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(live.URL, "/v4/ai/live-model?") || !strings.Contains(rt.URL, "/v4/ai/realtime-model?") {
		t.Fatalf("urls %s %s", live.URL, rt.URL)
	}
	want := []string{
		`{"model":"openai/gpt-live-1.1","routeKind":"live","expiresIn":60}`,
		`{"model":"openai/gpt-live-2","routeKind":"live"}`,
		`{"model":"openai/gpt-live-1.1"}`,
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("bodies %v", bodies)
	}
	if _, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "m", API: port2provgStr("x")}); !port2provgIsInvalidArg(err) {
		t.Fatalf("bad api: %v", err)
	}
}

// The contract with ai.ConnectRealtime (pkg/ai imports this package, so the
// optional interfaces are restated): a continuous model declaring
// client-secret-websocket and server-websocket.
func TestPortprovgLiveSatisfiesSessionContract(t *testing.T) {
	var model provider.Experimental_RealtimeModelV4 = port2provgLiveModel(t)
	conv, ok := model.(interface{ RealtimeConversation() string })
	if !ok || conv.RealtimeConversation() != "continuous" {
		t.Fatal("RealtimeConversationProvider")
	}
	conns, ok := model.(interface{ RealtimeConnections() []string })
	if !ok || !reflect.DeepEqual(conns.RealtimeConnections(), []string{"client-secret-websocket", "server-websocket"}) {
		t.Fatal("RealtimeConnectionsProvider")
	}
}
