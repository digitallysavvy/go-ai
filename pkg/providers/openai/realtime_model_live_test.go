package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports the intent of
// ai/packages/openai/src/live/openai-realtime-model-live.test.ts,
// openai-live-event-mapper.test.ts, openai-live-session-config.test.ts and
// realtime/openai-realtime-factory.test.ts (WebSocket-only; WebRTC is
// TS-only and not ported).

func TestOpenAIRealtimeFactory_RoutesLiveAndRealtime(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	cases := []struct {
		modelID  string
		opts     OpenAIRealtimeModelOptions
		wantLive bool
	}{
		{"gpt-live-1", OpenAIRealtimeModelOptions{}, true},
		{"gpt-realtime", OpenAIRealtimeModelOptions{}, false},
		{"unknown", OpenAIRealtimeModelOptions{}, false},
		{"gpt-live", OpenAIRealtimeModelOptions{}, false},
		{"gpt-live-1-preview", OpenAIRealtimeModelOptions{}, false},
		{"prefix-gpt-live-1", OpenAIRealtimeModelOptions{}, false},
		{"GPT-LIVE-1", OpenAIRealtimeModelOptions{}, false},
		{"not-yet-released", OpenAIRealtimeModelOptions{API: strPtr("live")}, true},
		{"gpt-realtime", OpenAIRealtimeModelOptions{API: strPtr("live")}, true},
		{"gpt-live-1", OpenAIRealtimeModelOptions{API: strPtr("realtime")}, false},
	}
	for _, tc := range cases {
		apiLabel := ""
		if tc.opts.API != nil {
			apiLabel = *tc.opts.API
		}
		t.Run(tc.modelID+"/"+apiLabel, func(t *testing.T) {
			model, err := p.ExperimentalRealtimeModel(tc.modelID, tc.opts)
			if err != nil {
				t.Fatalf("ExperimentalRealtimeModel() error = %v", err)
			}
			_, isLive := model.(*OpenAIRealtimeModelLive)
			if isLive != tc.wantLive {
				t.Fatalf("modelID=%q opts=%+v: isLive = %v, want %v", tc.modelID, tc.opts, isLive, tc.wantLive)
			}
			if model.ModelID() != tc.modelID {
				t.Fatalf("ModelID() = %q, want %q", model.ModelID(), tc.modelID)
			}
			if model.SpecificationVersion() != "v4" {
				t.Fatalf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
			}
		})
	}
}

func TestOpenAIRealtimeFactory_RejectsInvalidAPISelector(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	_, err := p.ExperimentalRealtimeModel("gpt-live-1", OpenAIRealtimeModelOptions{API: strPtr("invalid")})
	if err == nil || !strings.Contains(err.Error(), `OpenAI realtime api must be "live" or "realtime".`) {
		t.Fatalf("error = %v", err)
	}
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("error = %v, want InvalidArgumentError", err)
	}

	_, err = p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "gpt-live-1", API: strPtr("invalid")})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("GetRealtimeToken() error = %v, want InvalidArgumentError", err)
	}
}

// TestOpenAIRealtimeFactory_DistinguishesUnsetFromExplicitEmptyAPI covers a
// review finding: API is *string precisely so an omitted option (nil) can
// fall through to model-ID routing while an explicit empty string is
// rejected like any other unrecognized value, mirroring TS's
// resolveRealtimeApi (which only special-cases `api === undefined`, so an
// explicit ” throws). A string-typed API field with "" as its zero value
// could not distinguish the two.
func TestOpenAIRealtimeFactory_DistinguishesUnsetFromExplicitEmptyAPI(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	model, err := p.ExperimentalRealtimeModel("gpt-live-1", OpenAIRealtimeModelOptions{API: nil})
	if err != nil {
		t.Fatalf("nil API: unexpected error = %v", err)
	}
	if _, isLive := model.(*OpenAIRealtimeModelLive); !isLive {
		t.Fatalf("nil API: expected model-ID routing to select Live for gpt-live-1")
	}

	_, err = p.ExperimentalRealtimeModel("gpt-live-1", OpenAIRealtimeModelOptions{API: strPtr("")})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf(`explicit "" API: error = %v, want InvalidArgumentError`, err)
	}
}

func TestOpenAIRealtimeModelLive_DoCreateClientSecretRejected(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	_, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil || !strings.Contains(err.Error(), "Use server WebSocket setup via GetServerWebSocketConfig()") {
		t.Fatalf("error = %v", err)
	}
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestOpenAIRealtimeFactory_GetRealtimeToken_RejectsLive(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	_, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "gpt-live-1"})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestOpenAIRealtimeModelLive_GetServerWebSocketConfig(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	cfg, err := model.GetServerWebSocketConfig()
	if err != nil {
		t.Fatalf("GetServerWebSocketConfig() error = %v", err)
	}
	if cfg.URL != "wss://api.openai.com/v1/live/sessions" {
		t.Fatalf("URL = %q", cfg.URL)
	}
	if cfg.Headers["Authorization"] != "Bearer test-key" {
		t.Fatalf("headers = %v", cfg.Headers)
	}
}

func TestOpenAIRealtimeModelLive_GetServerWebSocketConfig_CustomBaseAndHeaders(t *testing.T) {
	p := New(Config{
		APIKey:       "test-key",
		BaseURL:      "https://example.com/proxy/v1/",
		Name:         "custom",
		Organization: "org-test",
		Project:      "proj-test",
		Headers:      map[string]string{"X-Custom": "value"},
	})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	if got, want := model.Provider(), "custom.live"; got != want {
		t.Fatalf("Provider() = %q, want %q", got, want)
	}
	cfg, err := model.GetServerWebSocketConfig()
	if err != nil {
		t.Fatalf("GetServerWebSocketConfig() error = %v", err)
	}
	if cfg.URL != "wss://example.com/proxy/v1/live/sessions" {
		t.Fatalf("URL = %q", cfg.URL)
	}
	if cfg.Headers["Authorization"] != "Bearer test-key" ||
		cfg.Headers["OpenAI-Organization"] != "org-test" ||
		cfg.Headers["OpenAI-Project"] != "proj-test" ||
		cfg.Headers["X-Custom"] != "value" {
		t.Fatalf("headers = %v", cfg.Headers)
	}
}

func TestOpenAIRealtimeModelLive_GetServerWebSocketConfig_HTTPMapsToWS(t *testing.T) {
	p := New(Config{APIKey: "test-key", BaseURL: "http://localhost:3000/v1"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	cfg, err := model.GetServerWebSocketConfig()
	if err != nil {
		t.Fatalf("GetServerWebSocketConfig() error = %v", err)
	}
	if cfg.URL != "ws://localhost:3000/v1/live/sessions" {
		t.Fatalf("URL = %q", cfg.URL)
	}
}

func TestOpenAIRealtimeModelLive_BuildsStartupPayload(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	instructions := "Be concise."
	voice := "marin"
	raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
		Type:    "session-start",
		EventID: "start-1",
		Config: provider.RealtimeSessionConfig{
			Instructions: &instructions,
			Voice:        &voice,
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"delegation": map[string]interface{}{"type": "client"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SerializeClientEvent() error = %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	want := map[string]interface{}{
		"type":     "session.start",
		"event_id": "start-1",
		"session": map[string]interface{}{
			"model":        "gpt-live-1",
			"instructions": "Be concise.",
			"audio": map[string]interface{}{
				"format": map[string]interface{}{"type": "audio/pcm", "rate": float64(24000)},
				"output": map[string]interface{}{"voice": "marin"},
			},
			"delegation": map[string]interface{}{"type": "client"},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestOpenAIRealtimeModelLive_BuildSessionConfig_StartupHistoryDelegationVoice(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	input := []interface{}{
		map[string]interface{}{"type": "message", "role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "Hello"}}},
		map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "Hi"}}},
	}
	got := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"delegation": map[string]interface{}{"type": "client"},
				"input":      input,
				"store":      true,
				"voice":      map[string]interface{}{"id": "voice-test"},
			},
		},
	})
	want := map[string]interface{}{
		"model": "gpt-live-1",
		"audio": map[string]interface{}{
			"format": map[string]interface{}{"type": "audio/pcm", "rate": float64(24000)},
			"output": map[string]interface{}{"voice": map[string]interface{}{"id": "voice-test"}},
		},
		"delegation": map[string]interface{}{"type": "client"},
		"input":      input,
		"store":      true,
	}
	assertJSONEqual(t, got, want)

	got2 := model.BuildSessionConfig(provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"delegation": nil, "store": false},
		},
	})
	m2, ok := got2.(map[string]interface{})
	if !ok {
		t.Fatalf("BuildSessionConfig() = %#v, want map", got2)
	}
	if _, present := m2["delegation"]; !present || m2["delegation"] != nil {
		t.Fatalf("delegation = %#v, want explicit nil", m2["delegation"])
	}
	if m2["store"] != false {
		t.Fatalf("store = %#v, want false", m2["store"])
	}
}

func TestOpenAIRealtimeModelLive_AudioFormats(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	rate16000, rate24000, rate8000 := 16000, 24000, 8000
	formats := []provider.RealtimeAudioFormat{
		{Type: "audio/pcm", Rate: &rate16000},
		{Type: "audio/pcm", Rate: &rate24000},
		{Type: "audio/pcmu", Rate: &rate8000},
		{Type: "audio/pcma", Rate: &rate8000},
	}
	for _, f := range formats {
		f := f
		got := model.BuildSessionConfig(provider.RealtimeSessionConfig{InputAudioFormat: &f, OutputAudioFormat: &f})
		m, ok := got.(map[string]interface{})
		if !ok {
			t.Fatalf("BuildSessionConfig() = %#v", got)
		}
		audio, _ := m["audio"].(map[string]interface{})
		format, _ := audio["format"].(map[string]interface{})
		if format["type"] != f.Type || format["rate"] != *f.Rate {
			t.Fatalf("format = %#v, want %+v", format, f)
		}
	}
}

func TestOpenAIRealtimeModelLive_RejectsMismatchedFormatsAndAmbiguousVoice(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	r24, r16 := 24000, 16000
	_, err := buildOpenAILiveSessionConfig(provider.RealtimeSessionConfig{
		InputAudioFormat:  &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &r24},
		OutputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &r16},
	}, model.modelID)
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("error = %v, want InvalidArgumentError", err)
	}

	voice := "marin"
	_, err = buildOpenAILiveSessionConfig(provider.RealtimeSessionConfig{
		Voice: &voice,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"voice": map[string]interface{}{"id": "voice-test"}},
		},
	}, model.modelID)
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("error = %v, want InvalidArgumentError", err)
	}
}

func TestOpenAIRealtimeModelLive_ValidatesStartupConfig(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	rate48000 := 48000
	tests := []provider.RealtimeSessionConfig{
		{InputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &rate48000}},
		{InputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/mp3"}},
		{ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"input": []interface{}{
				map[string]interface{}{"type": "message", "role": "system", "content": []interface{}{}},
			}},
		}},
		{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": "yes"}}},
		{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"unknown": true}}},
	}
	for i, cfg := range tests {
		if _, err := buildOpenAILiveSessionConfig(cfg, model.modelID); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestOpenAIRealtimeModelLive_RejectsUnsupportedVoiceTurnSettings(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	_, err := buildOpenAILiveSessionConfig(provider.RealtimeSessionConfig{
		TurnDetection: &provider.RealtimeTurnDetection{Type: "disabled"},
	}, model.modelID)
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v, want UnsupportedFunctionalityError", err)
	}
	// A non-empty Tools slice mirrors TS's `tools: [...]`; Go treats a nil
	// (omitted) or empty Tools slice as absent, since Go has no way to
	// distinguish an explicit `tools: []` from omission the way TS can.
	_, err = buildOpenAILiveSessionConfig(provider.RealtimeSessionConfig{
		Tools: []provider.RealtimeToolDefinition{{Name: "x", Parameters: map[string]interface{}{}}},
	}, model.modelID)
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestOpenAIRealtimeModelLive_SerializesSimpleClientEvents(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	cases := []struct {
		event provider.RealtimeClientEvent
		want  map[string]interface{}
	}{
		{provider.RealtimeClientEvent{Type: "session-close", EventID: "close-1"}, map[string]interface{}{"type": "session.close", "event_id": "close-1"}},
		{provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "AAAA"}, map[string]interface{}{"type": "session.input_audio.append", "audio": "AAAA"}},
		{provider.RealtimeClientEvent{Type: "input-audio-mute", EventID: "mute-1"}, map[string]interface{}{"type": "session.input_audio.mute", "event_id": "mute-1"}},
		{provider.RealtimeClientEvent{Type: "input-audio-unmute"}, map[string]interface{}{"type": "session.input_audio.unmute"}},
	}
	for _, tc := range cases {
		raw, err := model.SerializeClientEvent(tc.event)
		if err != nil {
			t.Fatalf("SerializeClientEvent(%+v) error = %v", tc.event, err)
		}
		var got map[string]interface{}
		_ = json.Unmarshal(raw, &got)
		assertJSONEqual(t, got, tc.want)
	}
}

func TestOpenAIRealtimeModelLive_SerializesContextAppend(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	for _, channel := range []string{"instructions", "thinking", "commentary"} {
		for _, delegationID := range []*string{nil, strPtr("opaque-delegation")} {
			raw, err := model.SerializeClientEvent(provider.RealtimeClientEvent{
				Type:            "context-append",
				Content:         "Context",
				DelegationID:    delegationID,
				EventID:         "append-1",
				ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"channel": channel}},
			})
			if err != nil {
				t.Fatalf("SerializeClientEvent() error = %v", err)
			}
			var got map[string]interface{}
			_ = json.Unmarshal(raw, &got)
			want := map[string]interface{}{
				"type":          "session." + channel + ".append",
				"content":       "Context",
				"event_id":      "append-1",
				"delegation_id": nil,
			}
			if delegationID != nil {
				want["delegation_id"] = *delegationID
			}
			assertJSONEqual(t, got, want)
		}
	}
}

func TestOpenAIRealtimeModelLive_RejectsSessionUpdateAndLegacyCommands(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewRealtimeModelLive(p, "gpt-live-1")
	for _, evtType := range []string{"session-update", "input-audio-commit", "input-audio-clear", "response-create", "response-cancel", "conversation-item-truncate"} {
		_, err := model.SerializeClientEvent(provider.RealtimeClientEvent{Type: evtType})
		if !providererrors.IsUnsupportedFunctionalityError(err) {
			t.Fatalf("type=%s: error = %v, want UnsupportedFunctionalityError", evtType, err)
		}
	}
}

func TestParseOpenAILiveServerEvent(t *testing.T) {
	usage12 := map[string]interface{}{"seconds": float64(12)}
	_ = usage12
	cases := []struct {
		name string
		raw  map[string]interface{}
		want provider.RealtimeServerEvent
	}{
		{
			"session-started",
			map[string]interface{}{"type": "session.started", "session": map[string]interface{}{"id": "session-1", "model": "gpt-live-1"}},
			provider.RealtimeServerEvent{Type: "session-started", SessionID: "session-1", DelegationMode: "client"},
		},
		{
			"session-closed",
			map[string]interface{}{"type": "session.closed", "session": map[string]interface{}{"id": "session-1"}, "usage": map[string]interface{}{"seconds": float64(12)}, "reason": "close_requested"},
			provider.RealtimeServerEvent{Type: "session-closed", SessionID: "session-1", Usage: &provider.RealtimeUsage{Seconds: 12}, Reason: "close_requested"},
		},
		{
			"session-usage",
			map[string]interface{}{"type": "session.usage.updated", "usage": map[string]interface{}{"seconds": float64(12)}, "context_window": map[string]interface{}{"usage_ratio": 0.42}},
			provider.RealtimeServerEvent{Type: "session-usage", Usage: &provider.RealtimeUsage{Seconds: 12}, ContextWindowUsageRatio: floatPtr(0.42)},
		},
		{
			"audio-chunk",
			map[string]interface{}{"type": "session.output_audio.delta", "delta": "AAAA"},
			provider.RealtimeServerEvent{Type: "audio-chunk", Delta: "AAAA"},
		},
		{
			"transcript-fragment-user",
			map[string]interface{}{"type": "session.input_transcript.delta", "delta": " hello", "start_ms": float64(100), "end_ms": float64(200)},
			provider.RealtimeServerEvent{Type: "transcript-fragment", Speaker: "user", Delta: " hello", StartMs: intPtr(100), EndMs: intPtr(200)},
		},
		{
			"delegation-created",
			map[string]interface{}{"type": "session.delegation.created", "offset_ms": float64(1000), "delegation": map[string]interface{}{"id": "opaque-delegation", "target": "client"}},
			provider.RealtimeServerEvent{Type: "delegation-created", DelegationID: "opaque-delegation", Target: "client", OffsetMs: intPtr(1000)},
		},
		{
			"command-acknowledged-mute",
			map[string]interface{}{"type": "session.input_audio.muted", "client_event_id": "mute-1"},
			provider.RealtimeServerEvent{Type: "command-acknowledged", Command: "session.input_audio.mute", ClientEventID: "mute-1"},
		},
		{
			"command-acknowledged-instructions",
			map[string]interface{}{"type": "session.instructions.appended", "client_event_id": "append-1", "start_ms": float64(10), "end_ms": float64(20)},
			provider.RealtimeServerEvent{Type: "command-acknowledged", Command: "session.instructions.append", ClientEventID: "append-1"},
		},
		{
			"error",
			map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "Rejected", "code": "immutable_field_update", "client_event_id": "update-1"}},
			provider.RealtimeServerEvent{Type: "error", Message: "Rejected", Code: "immutable_field_update", ClientEventID: "update-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.raw)
			got := parseOpenAILiveServerEvent(raw)
			if len(got) != 1 {
				t.Fatalf("parseOpenAILiveServerEvent() = %d events, want 1", len(got))
			}
			e := got[0]
			e.Raw = nil
			want := tc.want
			if e.Type != want.Type || e.SessionID != want.SessionID || e.Reason != want.Reason ||
				e.Delta != want.Delta || e.Speaker != want.Speaker || e.DelegationID != want.DelegationID ||
				e.Target != want.Target || e.Command != want.Command || e.ClientEventID != want.ClientEventID ||
				e.Message != want.Message || e.Code != want.Code || e.DelegationMode != want.DelegationMode {
				t.Fatalf("got  %+v\nwant %+v", e, want)
			}
			if !ptrIntEqual(e.StartMs, want.StartMs) || !ptrIntEqual(e.EndMs, want.EndMs) || !ptrIntEqual(e.OffsetMs, want.OffsetMs) {
				t.Fatalf("got  %+v\nwant %+v", e, want)
			}
			if !ptrFloatEqual(e.ContextWindowUsageRatio, want.ContextWindowUsageRatio) {
				t.Fatalf("ContextWindowUsageRatio got %v want %v", e.ContextWindowUsageRatio, want.ContextWindowUsageRatio)
			}
			if (e.Usage == nil) != (want.Usage == nil) || (e.Usage != nil && e.Usage.Seconds != want.Usage.Seconds) {
				t.Fatalf("Usage got %v want %v", e.Usage, want.Usage)
			}
		})
	}
}

func TestParseOpenAILiveServerEvent_InvalidKnownEvents(t *testing.T) {
	cases := []map[string]interface{}{
		{"type": "session.started"},
		{"type": "session.started", "session": map[string]interface{}{"id": ""}},
		{"type": "session.closed", "session": map[string]interface{}{"id": "session-1"}, "reason": "close_requested"},
		{"type": "session.closed", "usage": map[string]interface{}{"seconds": float64(1)}},
		{"type": "session.usage.updated", "usage": map[string]interface{}{"seconds": float64(-1)}},
		{"type": "session.usage.updated", "usage": map[string]interface{}{"seconds": "1"}},
		{"type": "session.output_audio.delta", "delta": float64(123)},
		{"type": "session.input_transcript.delta", "delta": "hi"},
		{"type": "session.output_transcript.delta", "delta": "hi", "start_ms": float64(20), "end_ms": float64(10)},
		{"type": "session.delegation.created", "delegation": map[string]interface{}{}},
		{"type": "session.updated"},
		{"type": "session.input_audio.muted", "client_event_id": float64(123)},
		{"type": "session.instructions.appended", "client_event_id": "append-1"},
		{"type": "error", "error": map[string]interface{}{"message": "bad", "code": float64(42)}},
		// The three ".appended" command acknowledgments carry start_ms/end_ms
		// like transcript deltas and must also reject end_ms < start_ms.
		{"type": "session.instructions.appended", "start_ms": float64(20), "end_ms": float64(10)},
		{"type": "session.thinking.appended", "start_ms": float64(20), "end_ms": float64(10)},
		{"type": "session.commentary.appended", "start_ms": float64(20), "end_ms": float64(10)},
		// delegation.target must be "client" or "responses" (an enum in TS),
		// not an arbitrary string.
		{"type": "session.delegation.created", "delegation": map[string]interface{}{"id": "d-1", "target": "bogus"}},
		// session.started's delegation.type is the same enum.
		{"type": "session.started", "session": map[string]interface{}{"id": "s-1", "delegation": map[string]interface{}{"type": "bogus"}}},
	}
	for i, c := range cases {
		raw, _ := json.Marshal(c)
		got := parseOpenAILiveServerEvent(raw)
		if len(got) != 1 || got[0].Type != "error" || got[0].Code != "invalid_server_event" {
			t.Fatalf("case %d (%v): got %+v, want invalid_server_event", i, c, got)
		}
	}
}

// TestParseOpenAILiveServerEvent_TimeIntervalMessageDistinctFromSchemaFailure
// mirrors the TypeScript SDK's two distinct invalid-event messages in
// parseServerEvent: a schema failure ('Invalid OpenAI Live server event.')
// vs a well-formed event whose end_ms < start_ms ('Invalid OpenAI Live event
// time interval.').
func TestParseOpenAILiveServerEvent_TimeIntervalMessageDistinctFromSchemaFailure(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"type": "session.output_transcript.delta", "delta": "hi",
		"start_ms": float64(20), "end_ms": float64(10),
	})
	got := parseOpenAILiveServerEvent(raw)
	if len(got) != 1 || got[0].Message != "Invalid OpenAI Live event time interval." {
		t.Fatalf("got %+v, want message 'Invalid OpenAI Live event time interval.'", got)
	}

	raw, _ = json.Marshal(map[string]interface{}{"type": "session.started"})
	got = parseOpenAILiveServerEvent(raw)
	if len(got) != 1 || got[0].Message != "Invalid OpenAI Live server event." {
		t.Fatalf("got %+v, want message 'Invalid OpenAI Live server event.'", got)
	}
}

func TestParseOpenAILiveServerEvent_UnknownTypesPassThroughAsCustom(t *testing.T) {
	for _, rawType := range []string{"future.event", "response.completed", "response.output_text.delta"} {
		raw, _ := json.Marshal(map[string]interface{}{"type": rawType, "additional": true})
		got := parseOpenAILiveServerEvent(raw)
		if len(got) != 1 || got[0].Type != "custom" || got[0].RawType != rawType {
			t.Fatalf("type=%s: got %+v", rawType, got)
		}
	}
}

func TestParseOpenAILiveServerEvent_DelegationMode(t *testing.T) {
	cases := []struct {
		delegation interface{}
		want       string
	}{
		{nil, "client"},
		{map[string]interface{}{"type": "client"}, "client"},
		{map[string]interface{}{"type": "responses"}, "provider"},
	}
	for _, tc := range cases {
		session := map[string]interface{}{"id": "session-1"}
		if tc.delegation != nil {
			session["delegation"] = tc.delegation
		}
		raw, _ := json.Marshal(map[string]interface{}{"type": "session.started", "session": session})
		got := parseOpenAILiveServerEvent(raw)
		if len(got) != 1 || got[0].DelegationMode != tc.want {
			t.Fatalf("delegation=%v: got %+v, want mode %q", tc.delegation, got, tc.want)
		}
	}
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }

func ptrIntEqual(a, b *int) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}

func ptrFloatEqual(a, b *float64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}

func assertJSONEqual(t *testing.T, got, want interface{}) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	var g, w interface{}
	_ = json.Unmarshal(gotJSON, &g)
	_ = json.Unmarshal(wantJSON, &w)
	gs, _ := json.Marshal(g)
	ws, _ := json.Marshal(w)
	if string(gs) != string(ws) {
		t.Fatalf("got  %s\nwant %s", gs, ws)
	}
}
