package mistral

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/net/websocket"
)

// Ported from mistral-chat-language-model.test.ts (TS 9ab42ea99a, #24350):
// Mistral Large 4 accepts low/medium/high reasoning_effort and cannot disable
// reasoning.
func TestR87provaLarge4ReasoningEffort(t *testing.T) {
	cases := []struct {
		level types.ReasoningLevel
		want  string
	}{
		{types.ReasoningMinimal, "low"}, {types.ReasoningLow, "low"},
		{types.ReasoningMedium, "medium"}, {types.ReasoningHigh, "high"},
		{types.ReasoningXHigh, "high"}, {types.ReasoningMax, "high"},
	}
	for _, id := range []string{ModelMistralLarge4, ModelMistralLarge40} {
		m := NewLanguageModel(New(Config{APIKey: "k"}), id)
		for _, c := range cases {
			level := c.level
			body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level, Prompt: types.Prompt{Text: "x"}}, false)
			if body["reasoning_effort"] != c.want {
				t.Errorf("%s %s: reasoning_effort = %v, want %s", id, level, body["reasoning_effort"], c.want)
			}
		}
		medium := types.ReasoningMedium
		if ws := m.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: &medium, Prompt: types.Prompt{Text: "x"}}); len(ws) != 0 {
			t.Errorf("%s medium: warnings = %#v, want none", id, ws)
		}

		// Native medium reasoning effort passes validation, is sent, no warnings.
		native := &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "x"},
			ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": "medium"}},
		}
		if err := validateChatProviderOptions(native.ProviderOptions); err != nil {
			t.Fatalf("%s: native medium rejected: %v", id, err)
		}
		if _, err := extractMistralProviderOptions(native); err != nil {
			t.Fatalf("%s: native medium rejected: %v", id, err)
		}
		if body := m.buildRequestBody(native, false); body["reasoning_effort"] != "medium" {
			t.Errorf("%s native medium: reasoning_effort = %v", id, body["reasoning_effort"])
		}
		if ws := m.checkReasoningWarnings(native); len(ws) != 0 {
			t.Errorf("%s native medium: warnings = %#v", id, ws)
		}

		none := types.ReasoningNone
		opts := &provider.GenerateOptions{Reasoning: &none, Prompt: types.Prompt{Text: "x"}}
		if body := m.buildRequestBody(opts, false); body["reasoning_effort"] != "low" {
			t.Errorf("%s none: reasoning_effort = %v, want low", id, body["reasoning_effort"])
		}
		ws := m.checkReasoningWarnings(opts)
		if len(ws) != 1 || ws[0].Type != "compatibility" || ws[0].Feature != "reasoning" ||
			ws[0].Details != `reasoning "none" is not directly supported by this model. mapped to effort "low".` {
			t.Errorf("%s none: warnings = %#v", id, ws)
		}
	}
}

// r87provaAudio is a provider.AudioStream over fixed chunks (an optional
// error after them) that records Cancel.
type r87provaAudio struct {
	mu        sync.Mutex
	chunks    [][]byte
	err       error
	block     chan struct{}
	cancelled int
}

func (a *r87provaAudio) Next(ctx context.Context) ([]byte, error) {
	a.mu.Lock()
	if len(a.chunks) > 0 {
		c := a.chunks[0]
		a.chunks = a.chunks[1:]
		a.mu.Unlock()
		return c, nil
	}
	err, block := a.err, a.block
	a.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (a *r87provaAudio) Cancel(error) {
	a.mu.Lock()
	a.cancelled++
	a.mu.Unlock()
}

// r87provaRealtimeServer is a Mistral realtime transcription server: it
// records the handshake and every client message and runs script once the
// client sent session.update and (when awaitEnd) input_audio.end.
type r87provaRealtimeServer struct {
	mu       sync.Mutex
	url      string
	header   http.Header
	received []map[string]interface{}
	srv      *httptest.Server
}

func (s *r87provaRealtimeServer) messages() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]interface{}(nil), s.received...)
}

func r87provaStartRealtime(t *testing.T, awaitEnd bool, script func(send func(string), conn *websocket.Conn)) *r87provaRealtimeServer {
	t.Helper()
	rs := &r87provaRealtimeServer{}
	rs.srv = httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		rs.mu.Lock()
		rs.url = conn.Request().URL.String()
		rs.header = conn.Request().Header.Clone()
		rs.mu.Unlock()
		send := func(msg string) { _ = websocket.Message.Send(conn, msg) }
		send(`{"type":"session.created"}`)
		for {
			var text string
			if err := websocket.Message.Receive(conn, &text); err != nil {
				return
			}
			var m map[string]interface{}
			_ = json.Unmarshal([]byte(text), &m)
			rs.mu.Lock()
			rs.received = append(rs.received, m)
			rs.mu.Unlock()
			switch m["type"] {
			case "session.update":
				send(`{"type":"session.updated"}`)
				if !awaitEnd {
					script(send, conn)
				}
			case "input_audio.end":
				if awaitEnd {
					script(send, conn)
				}
			}
		}
	}})
	t.Cleanup(rs.srv.Close)
	return rs
}

func r87provaRealtimeModel(t *testing.T, rs *r87provaRealtimeServer, cfg Config) *TranscriptionModel {
	t.Helper()
	if cfg.APIKey == "" && cfg.APIKeyFunc == nil {
		cfg.APIKey = "test-key"
	}
	cfg.BaseURL = rs.srv.URL + "/v1"
	return NewTranscriptionModel(New(cfg), ModelVoxtralMiniTranscribeRealtime3)
}

func r87provaDrain(t *testing.T, stream provider.TranscriptionStream) ([]provider.TranscriptionStreamPart, error) {
	t.Helper()
	var parts []provider.TranscriptionStreamPart
	done := make(chan error, 1)
	go func() {
		for {
			p, err := stream.Next()
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				done <- err
				return
			}
			parts = append(parts, *p)
		}
	}()
	select {
	case err := <-done:
		return parts, err
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not end")
		return nil, nil
	}
}

func r87provaRate(n int) *int { return &n }

// Ported from mistral-transcription-stream.test.ts (TS b26902ea1a).
func TestR87provaRealtimeTranscriptionConfiguresSendsAndFinishes(t *testing.T) {
	rs := r87provaStartRealtime(t, true, func(send func(string), _ *websocket.Conn) {
		send(`{"type":"transcription.text.delta","text":"Hello"}`)
		send(`{"type":"transcription.text.delta","text":" world."}`)
		send(`{"type":"transcription.done","text":"Hello world.","language":"en","model":"resolved-model","segments":[],"usage":{"prompt_audio_seconds":2,"total_tokens":5}}`)
	})
	m := r87provaRealtimeModel(t, rs, Config{Headers: map[string]string{"X-Provider": "provider"}})
	audio := &r87provaAudio{chunks: [][]byte{{1, 2}, {}, {3, 4}}}
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r87provaRate(24000)},
		Headers:          map[string]string{"X-Request": "request"},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"language": "en", "contextBias": []string{"AI SDK"}, "targetStreamingDelayMs": 480,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantURL := strings.Replace(rs.srv.URL, "http://", "ws://", 1) + "/v1/audio/transcriptions/realtime?model=voxtral-mini-transcribe-realtime-3"
	if res.RequestBody != wantURL || res.Response.ModelID != ModelVoxtralMiniTranscribeRealtime3 {
		t.Fatalf("request body = %v, response = %+v", res.RequestBody, res.Response)
	}
	parts, err := r87provaDrain(t, res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	if rs.url != "/v1/audio/transcriptions/realtime?model=voxtral-mini-transcribe-realtime-3" {
		t.Fatalf("url = %s", rs.url)
	}
	if rs.header.Get("Authorization") != "Bearer test-key" || rs.header.Get("X-Provider") != "provider" || rs.header.Get("X-Request") != "request" ||
		!strings.Contains(rs.header.Get("User-Agent"), "ai-sdk/mistral") && !strings.Contains(rs.header.Get("User-Agent"), "mistral") {
		t.Fatalf("headers = %v", rs.header)
	}
	got, _ := json.Marshal(rs.messages())
	want := `[{"session":{"audio_format":{"encoding":"pcm_s16le","sample_rate":24000},"context_bias":["AI SDK"],"language":"en","target_streaming_delay_ms":480},"type":"session.update"},{"audio":"AQI=","type":"input_audio.append"},{"audio":"AwQ=","type":"input_audio.append"},{"type":"input_audio.end"}]`
	if string(got) != want {
		t.Fatalf("client messages = %s\nwant %s", got, want)
	}
	var kinds []string
	for _, p := range parts {
		kinds = append(kinds, p.Type)
	}
	if strings.Join(kinds, ",") != "stream-start,transcript-delta,transcript-delta,response-metadata,finish" {
		t.Fatalf("parts = %v", kinds)
	}
	if parts[1].Delta != "Hello" || parts[2].Delta != " world." || parts[3].ModelID != "resolved-model" {
		t.Fatalf("parts = %+v", parts)
	}
	finish := parts[4]
	if finish.FinishText != "Hello world." || finish.Language != "en" || finish.DurationInSeconds == nil || *finish.DurationInSeconds != 2 ||
		finish.Usage["total_tokens"] != float64(5) || len(finish.Segments) != 0 {
		t.Fatalf("finish = %+v", finish)
	}
	if len(parts[0].Warnings) != 0 {
		t.Fatalf("warnings = %v", parts[0].Warnings)
	}
}

func TestR87provaRealtimeTranscriptionSegmentsAndRawEvents(t *testing.T) {
	rs := r87provaStartRealtime(t, true, func(send func(string), _ *websocket.Conn) {
		send(`{"type":"turn.start","time_s":1}`)
		send(`{"type":"future.event","new_field":true}`)
		send(`{"type":"transcription.segment","text":" Hi.","start":null,"end":null,"speaker_id":"Jane"}`)
		send(`{"type":"transcription.segment","text":" Hello.","start":1,"end":2,"speaker_id":"speaker_2"}`)
		send(`{"type":"transcription.done","text":" Hi. Hello.","segments":[{"text":" Hi.","start":null,"end":null,"speaker_id":"Jane"},{"text":" Hello.","start":1,"end":2,"speaker_id":"speaker_2"}],"language":null,"usage":null}`)
	})
	m := r87provaRealtimeModel(t, rs, Config{})
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &r87provaAudio{},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		IncludeRawChunks: true,
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"diarize": true, "speakerReferences": map[string]string{"Jane": "AQI="},
			"turnDetection":  map[string]interface{}{"startThreshold": 0.5, "eagerEndThreshold": 0.4, "endThreshold": 0.3, "minSilenceMs": 300, "endTimeoutMs": 2500},
			"returnLanguage": true, "temperature": 0.2, "timestampGranularities": []string{"segment"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r87provaDrain(t, res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := json.Marshal(rs.messages()[0]["session"])
	if string(session) != `{"audio_format":{"encoding":"pcm_s16le","sample_rate":16000},"diarize":true,"speaker_references":{"Jane":"AQI="},"turn_detection":{"eager_end_threshold":0.4,"end_threshold":0.3,"end_timeout_ms":2500,"min_silence_ms":300,"start_threshold":0.5}}` {
		t.Fatalf("session = %s", session)
	}
	var features []string
	for _, w := range parts[0].Warnings {
		features = append(features, w.Feature+"|"+w.Details)
	}
	if strings.Join(features, ",") != "providerOptions.mistral.returnLanguage|This option is not supported for realtime transcription.,providerOptions.mistral.temperature|This option is not supported for realtime transcription.,providerOptions.mistral.timestampGranularities|This option is not supported for realtime transcription." {
		t.Fatalf("warnings = %v", features)
	}
	var raws, finals []provider.TranscriptionStreamPart
	for _, p := range parts {
		switch p.Type {
		case "raw":
			raws = append(raws, p)
		case "transcript-final":
			finals = append(finals, p)
		}
	}
	// session.created, session.updated, 2 unknown, 2 segments, done.
	if len(raws) != 7 {
		t.Fatalf("raw parts = %d", len(raws))
	}
	if finals[0].Text != " Hi." || finals[0].StartSecond != nil || finals[0].EndSecond != nil ||
		!reflect.DeepEqual(finals[0].ProviderMetadata, map[string]interface{}{"mistral": map[string]interface{}{"speakerId": "Jane"}}) {
		t.Fatalf("final = %+v", finals[0])
	}
	finish := parts[len(parts)-1]
	if !reflect.DeepEqual(finish.Segments, []provider.TranscriptSegment{{Text: " Hello.", StartSecond: 1, EndSecond: 2}}) || finish.Language != "" || finish.Usage != nil {
		t.Fatalf("finish = %+v", finish)
	}
	meta, _ := json.Marshal(finish.ProviderMetadata)
	if string(meta) != `{"mistral":{"segments":[{"speakerId":"Jane","text":" Hi."},{"endSecond":2,"speakerId":"speaker_2","startSecond":1,"text":" Hello."}]}}` {
		t.Fatalf("finish metadata = %s", meta)
	}
}

func TestR87provaRealtimeTranscriptionSplitsLargeChunks(t *testing.T) {
	rs := r87provaStartRealtime(t, true, func(send func(string), _ *websocket.Conn) {
		send(`{"type":"transcription.done","text":"x"}`)
	})
	bytesIn := bytes.Repeat([]byte{7}, 300000)
	res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: &r87provaAudio{chunks: [][]byte{{}, bytesIn}}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r87provaDrain(t, res.Stream); err != nil {
		t.Fatal(err)
	}
	var joined []byte
	appends := 0
	for _, m := range rs.messages() {
		if m["type"] == "input_audio.append" {
			appends++
			s := m["audio"].(string)
			if len(s) >= 262144 {
				t.Fatalf("message too large: %d", len(s))
			}
			b, _ := base64.StdEncoding.DecodeString(s)
			joined = append(joined, b...)
		}
	}
	if appends != 5 || !bytes.Equal(joined, bytesIn) {
		t.Fatalf("appends = %d, %d bytes", appends, len(joined))
	}
}

func TestR87provaRealtimeTranscriptionFailures(t *testing.T) {
	cases := map[string]struct {
		script func(send func(string), conn *websocket.Conn)
		want   string
	}{
		"provider-error": {func(send func(string), conn *websocket.Conn) {
			send(`{"type":"error","error":{"message":"invalid model","code":400}}`)
			_ = conn.Close()
		}, "invalid model"},
		"close":        {func(_ func(string), conn *websocket.Conn) { _ = conn.WriteClose(4000) }, "Mistral realtime transcription connection closed before transcription.done (code: 4000)."},
		"invalid-json": {func(send func(string), _ *websocket.Conn) { send(`{`) }, "JSON parsing failed"},
		"invalid-event": {func(send func(string), _ *websocket.Conn) {
			send(`{"type":"transcription.text.delta","text":123}`)
		}, "Type validation failed"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rs := r87provaStartRealtime(t, false, c.script)
			audio := &r87provaAudio{block: make(chan struct{})}
			res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = r87provaDrain(t, res.Stream)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			audio.mu.Lock()
			cancelled := audio.cancelled
			audio.mu.Unlock()
			if cancelled != 1 {
				t.Fatalf("audio cancelled %d times", cancelled)
			}
		})
	}

	t.Run("abort", func(t *testing.T) {
		rs := r87provaStartRealtime(t, false, func(func(string), *websocket.Conn) {})
		ctx, cancel := context.WithCancelCause(context.Background())
		audio := &r87provaAudio{block: make(chan struct{})}
		res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(ctx, &provider.TranscriptionStreamOptions{
			Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		})
		if err != nil {
			t.Fatal(err)
		}
		time.AfterFunc(100*time.Millisecond, func() { cancel(errors.New("cancelled")) })
		if _, err := r87provaDrain(t, res.Stream); err == nil || err.Error() != "cancelled" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestR87provaRealtimeTranscriptionRejectsBeforeConnecting(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), ModelVoxtralMiniTranscribeRealtime3)
	for _, f := range []provider.AudioFormat{{Type: "audio/mpeg"}, {Type: "audio/pcm", Rate: r87provaRate(0)}} {
		_, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{InputAudioFormat: f})
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "requires audio/pcm") {
			t.Fatalf("%v: err = %v", f, err)
		}
	}
	for _, delay := range []interface{}{79, 1201, 2399, 2401, 480.5} {
		_, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			ProviderOptions:  map[string]interface{}{"mistral": map[string]interface{}{"targetStreamingDelayMs": delay}},
		})
		if err == nil {
			t.Fatalf("delay %v accepted", delay)
		}
	}
	for _, delay := range []interface{}{80, 1200, 2400} {
		o, err := parseMistralTranscriptionOptions(map[string]interface{}{"mistral": map[string]interface{}{"targetStreamingDelayMs": delay, "turnDetection": map[string]interface{}{}}})
		if err != nil {
			t.Fatalf("delay %v rejected: %v", delay, err)
		}
		session, _ := json.Marshal(mistralRealtimeTranscriptionSession(16000, o))
		if !strings.Contains(string(session), fmt.Sprintf(`"target_streaming_delay_ms":%v,"turn_detection":{}`, delay)) {
			t.Fatalf("session = %s", session)
		}
	}
}

// TS mistral-transcription-model.test.ts (b26902ea1a): the realtime-only
// options warn on the REST endpoint and are not sent.
func TestR87provaTranscribeWarnsRealtimeOnlyOptions(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"voxtral-mini-latest","text":"hi"}`)
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelVoxtralMiniLatest)
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"targetStreamingDelayMs": 480, "speakerReferences": map[string]interface{}{"Jane": "AQI="}, "turnDetection": map[string]interface{}{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range res.Warnings {
		got = append(got, w.Type+"|"+w.Feature+"|"+w.Details)
	}
	want := []string{
		"unsupported|providerOptions.mistral.targetStreamingDelayMs|This option is only supported for realtime transcription.",
		"unsupported|providerOptions.mistral.speakerReferences|This option is only supported for realtime transcription.",
		"unsupported|providerOptions.mistral.turnDetection|This option is only supported for realtime transcription.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v", got)
	}
	if strings.Contains(form, "480") || strings.Contains(form, "Jane") {
		t.Fatalf("realtime options sent: %s", form)
	}
}

func r87provaStr(s string) *string { return &s }

// Ported from realtime/mistral-realtime-model.test.ts (TS 0912e0b1b9).
func TestR87provaRealtimeSessions(t *testing.T) {
	type call struct {
		method, path, auth string
		body               map[string]interface{}
	}
	var calls []call
	status, respBody := 200, `{"id":"session-1","participant_token":"participant-token","server_url":"wss://voice.example.com","unused":"x"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	defer srv.Close()
	rt := New(Config{APIKey: "test-api-key", BaseURL: srv.URL + "/v1/"}).ExperimentalRealtime()

	session, err := rt.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "mistral-small-latest"})
	if err != nil {
		t.Fatal(err)
	}
	if *session != (RealtimeSession{ID: "session-1", ParticipantToken: "participant-token", ServerURL: "wss://voice.example.com"}) {
		t.Fatalf("session = %+v", session)
	}
	if c := calls[0]; c.method != "POST" || c.path != "/v1/audio/sessions" || c.auth != "Bearer test-api-key" ||
		!reflect.DeepEqual(c.body, map[string]interface{}{"llm": map[string]interface{}{"model": "mistral-small-latest"}}) {
		t.Fatalf("create call = %+v", c)
	}

	// Normalized and provider-specific settings.
	calls = nil
	_, err = rt.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "mistral-small-latest", SessionConfig: &provider.RealtimeSessionConfig{
		Instructions:             r87provaStr("Be concise."),
		Voice:                    r87provaStr("voice_447"),
		OutputModalities:         []string{"audio", "text"},
		OutputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{},
		InputAudioTranscription:  &provider.RealtimeAudioTranscriptionConfig{Model: r87provaStr("voxtral-mini-transcribe-realtime-3"), Language: r87provaStr("en")},
		Tools:                    []provider.RealtimeToolDefinition{{Type: "function", Name: "weather", Parameters: map[string]interface{}{"type": "object"}}},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"stt": map[string]interface{}{"model": "overridden-model", "language": "fr", "contextBias": []string{"Vercel"}, "targetStreamingDelayMs": 240,
				"turnDetection": map[string]interface{}{"minSilenceMs": 500, "endThreshold": 0.33}},
			"tts":            map[string]interface{}{"model": "voxtral-mini-tts-3", "voiceId": "overridden-voice", "phonemeHints": map[string][]string{"SDK": {"s"}}},
			"turn":           map[string]interface{}{"allowInterruptions": false, "inactivity": map[string]interface{}{"timeoutMs": 10000, "maxReminders": 2, "messages": []string{"Hello?"}}},
			"welcomeMessage": "Hello!", "agentId": "saved-agent", "agentVersion": 2,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"agent_id":"saved-agent","agent_version":2,"instructions":"Be concise.","llm":{"model":"mistral-small-latest"},"stt":{"context_bias":["Vercel"],"language":"en","model":"voxtral-mini-transcribe-realtime-3","target_streaming_delay_ms":240,"turn_detection":{"end_threshold":0.33,"min_silence_ms":500}},"tools":[{"function":{"name":"weather","parameters":{"type":"object"}},"type":"function"}],"tts":{"model":"voxtral-mini-tts-3","phoneme_hints":{"SDK":["s"]},"voice_id":"voice_447"},"turn":{"allow_interruptions":false,"inactivity":{"max_reminders":2,"messages":["Hello?"],"timeout_ms":10000}},"welcome_message":"Hello!"}`
	if got, _ := json.Marshal(calls[0].body); string(got) != want {
		t.Fatalf("body = %s\nwant %s", got, want)
	}

	// Close: the session ID is one encoded path segment.
	calls, status, respBody = nil, 204, ""
	if err := rt.CloseSession(context.Background(), "session/1?query"); err != nil {
		t.Fatal(err)
	}
	if c := calls[0]; c.method != "DELETE" || c.path != "/v1/audio/sessions/session%2F1%3Fquery" || c.auth != "Bearer test-api-key" {
		t.Fatalf("close call = %+v", c)
	}
	for _, id := range []string{"", ".", "..", " "} {
		if err := rt.CloseSession(context.Background(), id); err == nil || !strings.Contains(err.Error(), "valid session ID") {
			t.Fatalf("%q: err = %v", id, err)
		}
	}
	if _, err := rt.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: " "}); err == nil {
		t.Fatal("blank model accepted")
	}

	// HTTP errors and malformed credentials.
	status, respBody = 401, `{"object":"error","message":"Invalid key","type":"authentication","param":null,"code":null}`
	for _, op := range []func() error{
		func() error {
			_, err := rt.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "m"})
			return err
		},
		func() error { return rt.CloseSession(context.Background(), "session-1") },
	} {
		err := op()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 401 || !strings.Contains(err.Error(), "Invalid key") {
			t.Fatalf("err = %v", err)
		}
	}
	status, respBody = 200, `{"id":"session-1"}`
	if _, err := rt.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "m"}); err == nil || !strings.Contains(err.Error(), "Invalid JSON response") {
		t.Fatalf("malformed credentials: err = %v", err)
	}
}

func TestR87provaRealtimeSessionConfigChecks(t *testing.T) {
	var unsupported *providererrors.UnsupportedFunctionalityError
	for i, c := range []provider.RealtimeSessionConfig{
		{InputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm"}},
		{OutputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm"}},
		{InputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{Prompt: r87provaStr("Vocabulary")}},
		{OutputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{Model: r87provaStr("other")}},
		{OutputModalities: []string{"text"}},
		{TurnDetection: &provider.RealtimeTurnDetection{Type: "server-vad"}},
	} {
		if _, err := BuildRealtimeSessionConfig(c, "", false); !errors.As(err, &unsupported) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
	for i, m := range []map[string]interface{}{
		{"stt": map[string]interface{}{"targetStreamingDelayMs": 79}},
		{"stt": map[string]interface{}{"turnDetection": map[string]interface{}{"endThreshold": 2}}},
		{"tts": map[string]interface{}{"expressiveness": 2}},
		{"agentVersion": 2},
		{"unknown": true},
	} {
		if _, err := BuildRealtimeSessionConfig(provider.RealtimeSessionConfig{ProviderOptions: map[string]interface{}{"mistral": m}}, "", false); err == nil {
			t.Errorf("provider case %d accepted", i)
		}
	}
	got, err := BuildRealtimeSessionConfig(provider.RealtimeSessionConfig{
		Instructions:    r87provaStr("Updated instructions"),
		Tools:           []provider.RealtimeToolDefinition{},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"turn": map[string]interface{}{"allowInterruptions": true}}},
	}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(got); string(b) != `{"instructions":"Updated instructions","tools":[],"turn":{"allow_interruptions":true}}` {
		t.Fatalf("update = %s", b)
	}
	for i, c := range []provider.RealtimeSessionConfig{
		{Voice: r87provaStr("voice_447")},
		{ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"stt": map[string]interface{}{"language": "en"}}}},
		{ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"agentId": "saved-agent"}}},
	} {
		if _, err := BuildRealtimeSessionConfig(c, "", true); !errors.As(err, &unsupported) {
			t.Errorf("update case %d: err = %v", i, err)
		}
	}
}
