package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS b0fccbda62 (prepare-google-interactions-tools.test.ts, google-
// interactions-language-model.test.ts): google.tools.mcpServer exists with the
// id google.mcp_server, and the Interactions request still sends the real
// header values (only the telemetry copy is redacted, see pkg/telemetry).
func TestPortprovgMCPServerToolConstructor(t *testing.T) {
	args := map[string]interface{}{
		"name":         "local-mcp",
		"url":          "https://example.com/mcp",
		"headers":      map[string]string{"Authorization": "Bearer test-secret", "X-Custom-Key": "test-secret"},
		"allowedTools": []string{"getWeather"},
	}
	tool := MCPServerTool(args)
	if tool.ProviderID != "google.mcp_server" || tool.Name != "mcp_server" || !tool.ProviderExecuted || tool.Type != "provider" {
		t.Fatalf("tool = %+v", tool)
	}
	mapped, warning := mapInteractionsProviderTool(tool)
	if warning != nil {
		t.Fatalf("warning: %+v", warning)
	}
	raw, _ := json.Marshal(mapped)
	want := `{"allowed_tools":["getWeather"],"headers":{"Authorization":"Bearer test-secret","X-Custom-Key":"test-secret"},"name":"local-mcp","type":"mcp_server","url":"https://example.com/mcp"}`
	if string(raw) != want {
		t.Fatalf("wire tool = %s", raw)
	}
	if args["headers"].(map[string]string)["Authorization"] != "Bearer test-secret" {
		t.Fatal("tool args were modified")
	}
}

func TestPortprovgInteractionsRequestKeepsMCPCredentials(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"v1","status":"completed","steps":[],"usage":{"total_tokens":1}}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model := NewInteractionsLanguageModel(p, ModelGemini25Flash)
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{MCPServerTool(map[string]interface{}{"url": "https://example.com/mcp", "headers": map[string]string{"Authorization": "Bearer test-secret"}})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"Authorization":"Bearer test-secret"`) {
		t.Fatalf("request body lost the credential: %s", body)
	}
}

const portprovgGenerateResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
const portprovgInteractionResponse = `{"id":"v1","status":"completed","steps":[],"usage":{"total_tokens":1}}`

func portprovgServer(t *testing.T, resp string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

func portprovgLevel(r types.ReasoningLevel) *types.ReasoningLevel { return &r }

func portprovgWarningDetails(ws []types.Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Type+"/"+w.Feature+": "+w.Details)
	}
	return out
}

// TS google-language-model.test.ts "Gemini 4 Hydrogen" and the xhigh
// it.each rows (4e43721f73): Hydrogen and Neon accept xhigh and have no
// minimal level; other Gemini 4 models cap at high with a compatibility
// warning.
func TestPortprovgGemini4ThinkingLevels(t *testing.T) {
	cases := []struct {
		model     string
		reasoning types.ReasoningLevel
		want      string
		warns     bool
	}{
		{"gemini-4-hydrogen", types.ReasoningLow, "low", false},
		{"gemini-4-hydrogen", types.ReasoningMedium, "medium", false},
		{"gemini-4-hydrogen", types.ReasoningHigh, "high", false},
		{"gemini-4-hydrogen", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-hydrogen-eap", types.ReasoningXHigh, "xhigh", false},
		{"models/gemini-4-hydrogen-eap", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-hydrogen", types.ReasoningMax, "xhigh", true},
		{"gemini-4-hydrogen", types.ReasoningMinimal, "low", true},
		{"gemini-4-neon", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-argon", types.ReasoningXHigh, "high", true},
		{"gemini-4-carbon", types.ReasoningXHigh, "high", true},
		{"gemini-4-hydrogenous", types.ReasoningXHigh, "high", true},
	}
	for _, tc := range cases {
		t.Run(tc.model+"/"+string(tc.reasoning), func(t *testing.T) {
			server, body := portprovgServer(t, portprovgGenerateResponse)
			model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), tc.model)
			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: portprovgLevel(tc.reasoning)})
			if err != nil {
				t.Fatal(err)
			}
			gc, _ := (*body)["generationConfig"].(map[string]interface{})
			tcfg, _ := gc["thinkingConfig"].(map[string]interface{})
			if tcfg["thinkingLevel"] != tc.want {
				t.Fatalf("thinkingLevel = %v, want %s (body %v)", tcfg["thinkingLevel"], tc.want, *body)
			}
			if tc.warns != (len(result.Warnings) > 0) {
				t.Fatalf("warnings = %v", portprovgWarningDetails(result.Warnings))
			}
		})
	}
}

// "passes an explicit xhigh thinking level instead of portable reasoning" and
// "leaves the thinking level unset for the API default".
func TestPortprovgHydrogenExplicitAndDefaultThinking(t *testing.T) {
	server, body := portprovgServer(t, portprovgGenerateResponse)
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini4Hydrogen)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       portprovgLevel(types.ReasoningLow),
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingLevel": "xhigh"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gc, _ := (*body)["generationConfig"].(map[string]interface{})
	if tcfg, _ := gc["thinkingConfig"].(map[string]interface{}); tcfg["thinkingLevel"] != "xhigh" || len(result.Warnings) != 0 {
		t.Fatalf("explicit: body %v warnings %v", *body, result.Warnings)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	gc, _ = (*body)["generationConfig"].(map[string]interface{})
	if _, has := gc["thinkingConfig"]; has {
		t.Fatalf("default call sent thinkingConfig: %v", gc)
	}
}

// TS google-interactions-language-model.test.ts (4e43721f73, 8e60cb4ba3):
// xhigh thinkingLevel passes for Hydrogen, top-level reasoning maps onto the
// model's levels, other models warn and fall back, agent calls drop it.
func TestPortprovgInteractionsReasoning(t *testing.T) {
	type tc struct {
		name      string
		model     string
		reasoning *types.ReasoningLevel
		option    string
		want      interface{}
		warnings  []string
	}
	cases := []tc{
		{"option xhigh hydrogen", "gemini-4-hydrogen", nil, "xhigh", "xhigh", nil},
		{"option xhigh hydrogen-eap", "gemini-4-hydrogen-eap", nil, "xhigh", "xhigh", nil},
		{"option wins over reasoning", "gemini-2.5-flash", portprovgLevel(types.ReasoningXHigh), "high", "high", nil},
		{"reasoning hydrogen", "gemini-4-hydrogen", portprovgLevel(types.ReasoningXHigh), "", "xhigh", nil},
		{"reasoning argon falls back", "gemini-4-argon", portprovgLevel(types.ReasoningXHigh), "", "high",
			[]string{`compatibility/reasoning: reasoning "xhigh" is not directly supported by this model. mapped to effort "high".`}},
		{"reasoning on a budget model", "gemini-2.5-flash", portprovgLevel(types.ReasoningHigh), "", nil,
			[]string{`unsupported/reasoning: reasoning "high" is not supported by this model.`}},
		{"provider-default reasoning", "gemini-4-hydrogen", portprovgLevel(types.ReasoningDefault), "", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				server, body := portprovgServer(t, portprovgInteractionResponse)
				model := NewInteractionsLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), c.model)
				opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: c.reasoning}
				if c.option != "" {
					opts.ProviderOptions = map[string]interface{}{"google": map[string]interface{}{"thinkingLevel": c.option}}
				}
				var warnings []types.Warning
				if stream {
					// Only the request body matters on the stream path.
					server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						raw, _ := io.ReadAll(r.Body)
						_ = json.Unmarshal(raw, body)
						w.Header().Set("Content-Type", "text/event-stream")
						writeSSE(w, `{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","usage":{"total_tokens":1}}}`)
					})
					s, err := model.DoStream(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					_ = s.Close()
				} else {
					result, err := model.DoGenerate(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					warnings = result.Warnings
				}
				gc, _ := (*body)["generation_config"].(map[string]interface{})
				if gc["thinking_level"] != c.want {
					t.Fatalf("stream=%v thinking_level = %v, want %v (body %v)", stream, gc["thinking_level"], c.want, *body)
				}
				if !stream && strings.Join(portprovgWarningDetails(warnings), "|") != strings.Join(c.warnings, "|") {
					t.Fatalf("warnings = %v, want %v", portprovgWarningDetails(warnings), c.warnings)
				}
			}
		})
	}
}

func TestPortprovgInteractionsAgentDropsReasoning(t *testing.T) {
	server, _ := portprovgServer(t, portprovgInteractionResponse)
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatal(err)
	}
	im := model.(*InteractionsLanguageModel)
	_, warnings, _, err := im.buildArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: portprovgLevel(types.ReasoningHigh)}, false)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range warnings {
		if strings.Contains(w.Message, "reasoning") && strings.Contains(w.Message, "not supported when an agent is set") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no agent warning naming reasoning: %v", warnings)
	}
}

// TS 1145cd58eb row: gemini-nano-banana-2.1 takes no function tools, so a
// function tool next to a provider tool is not combined.
func TestPortprovgNanoBananaHasNoMixedTools(t *testing.T) {
	server, _ := portprovgServer(t, portprovgGenerateResponse)
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGeminiNanoBanana21)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			GoogleSearchTool(),
			{Type: types.ToolTypeFunction, Name: "f", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range result.Warnings {
		if w.Feature == "combination of function and provider-defined tools" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v", portprovgWarningDetails(result.Warnings))
	}
}

// TS google-language-model.test.ts (a4de3a2148): the prompt block reason
// message reaches providerMetadata.google.promptFeedback in generate and
// stream results.
func TestPortprovgPromptBlockReasonMessage(t *testing.T) {
	const feedback = `"promptFeedback":{"blockReason":"PROHIBITED_CONTENT","blockReasonMessage":"The prompt is blocked due to prohibited contents"},"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9}`
	want := `{"blockReason":"PROHIBITED_CONTENT","blockReasonMessage":"The prompt is blocked due to prohibited contents"}`
	feedbackJSON := func(t *testing.T, metadata interface{}) string {
		t.Helper()
		raw, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Google struct {
				PromptFeedback json.RawMessage `json:"promptFeedback"`
			} `json:"google"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		return string(decoded.Google.PromptFeedback)
	}

	t.Run("generate", func(t *testing.T) {
		server, _ := portprovg2Server(t, "application/json", "{"+feedback+"}")
		model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25Flash)
		result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := feedbackJSON(t, result.ProviderMetadata); got != want {
			t.Fatalf("promptFeedback = %s", got)
		}
	})
	t.Run("stream", func(t *testing.T) {
		server, _ := portprovg2Server(t, "text/event-stream", "data: {"+feedback+"}\n\n")
		model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25Flash)
		stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close() //nolint:errcheck
		var got string
		for {
			chunk, err := stream.Next()
			if err != nil {
				break
			}
			if chunk.Type == provider.ChunkTypeFinish {
				got = feedbackJSON(t, chunk.ProviderMetadata)
			}
		}
		if got != want {
			t.Fatalf("stream promptFeedback = %s", got)
		}
	})
}

func portprovg2Server(t *testing.T, contentType, body string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

const portprovgBatchOperation = `{"name":"batches/b1","done":false,"metadata":{"state":"BATCH_STATE_PENDING","createTime":"2026-08-04T12:34:56.123Z","batchStats":{"requestCount":"1","pendingRequestCount":"1"}}}`

func portprovgBatchRequest(format *provider.ResponseFormat, providerOptions map[string]interface{}) provider.BatchV4Request {
	return provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID:      "schema",
			ModelID: "gemini-2.5-flash",
			Options: provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, ResponseFormat: format, ProviderOptions: providerOptions},
		},
	}
}

func portprovgGenerationConfig(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	batch, _ := body["batch"].(map[string]interface{})
	inputConfig, _ := batch["inputConfig"].(map[string]interface{})
	wrapper, _ := inputConfig["requests"].(map[string]interface{})
	requests, _ := wrapper["requests"].([]interface{})
	if len(requests) != 1 {
		t.Fatalf("requests = %v", requests)
	}
	request, _ := requests[0].(map[string]interface{})["request"].(map[string]interface{})
	gc, _ := request["generationConfig"].(map[string]interface{})
	return gc
}

// TS google-batch.test.ts (3bea041f28, fe1b382b8e): a response schema is
// rejected before any request, unless structuredOutputs is false, and JSON
// mode without a schema stays allowed.
func TestPortprovgGoogleBatchResponseSchema(t *testing.T) {
	schema := &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}

	t.Run("rejects response schemas", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
		defer server.Close()
		b := New(Config{APIKey: "k", BaseURL: server.URL}).ExperimentalBatch()
		_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{portprovgBatchRequest(schema, nil)}})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) {
			t.Fatalf("err = %v (%T)", err, err)
		}
		if unsupported.Functionality != "structured output schemas in Google batches" || !strings.Contains(unsupported.Message, "providerOptions.google.structuredOutputs") || !strings.Contains(unsupported.Message, `(request "schema")`) {
			t.Fatalf("error = %+v", unsupported)
		}
		if calls != 0 {
			t.Fatalf("%d requests were sent", calls)
		}
	})

	for name, tc := range map[string]struct {
		format  *provider.ResponseFormat
		options map[string]interface{}
		schema  bool
	}{
		"allows a schema when structuredOutputs is disabled and does not send it": {schema, map[string]interface{}{"google": map[string]interface{}{"structuredOutputs": false}}, false},
		"allows JSON mode without a schema":                                       {&provider.ResponseFormat{Type: "json"}, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			var body map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, portprovgBatchOperation)
			}))
			defer server.Close()
			b := New(Config{APIKey: "k", BaseURL: server.URL}).ExperimentalBatch()
			if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{portprovgBatchRequest(tc.format, tc.options)}}); err != nil {
				t.Fatal(err)
			}
			gc := portprovgGenerationConfig(t, body)
			if gc["responseMimeType"] != "application/json" {
				t.Fatalf("generationConfig = %v", gc)
			}
			if _, has := gc["responseJsonSchema"]; has {
				t.Fatalf("schema was sent: %v", gc)
			}
		})
	}
}

// ---- live transcription (TS 378af05f85, google-transcription-model.test.ts) ----

type portprovgLive struct {
	t       *testing.T
	server  *liveTranscriptionTestServer
	audio   *chanAudioStream
	result  *provider.TranscriptionStreamResult
	done    chan struct{}
	partsMu sync.Mutex
	parts   []*provider.TranscriptionStreamPart
	err     error
}

func portprovgStartLive(t *testing.T, providerOptions map[string]interface{}, grace time.Duration) *portprovgLive {
	t.Helper()
	server := newLiveTranscriptionTestServer(t)
	t.Cleanup(server.close)
	model := newLiveTestModel(t, server.ts.URL)
	model.finishGrace = grace
	audio := &chanAudioStream{chunks: make(chan []byte, 8)}
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions:  providerOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Stream.Close() })
	l := &portprovgLive{t: t, server: server, audio: audio, result: result, done: make(chan struct{})}
	first, err := result.Stream.Next() // stream-start unblocks the message loop
	if err != nil {
		t.Fatal(err)
	}
	l.parts = append(l.parts, first)
	go func() {
		defer close(l.done)
		for {
			part, err := result.Stream.Next()
			l.partsMu.Lock()
			if err != nil {
				if err != io.EOF {
					l.err = err
				}
				l.partsMu.Unlock()
				return
			}
			l.parts = append(l.parts, part)
			l.partsMu.Unlock()
			if part.Type == provider.TranscriptionStreamPartTypeFinish {
				return
			}
		}
	}()
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	return l
}

func (l *portprovgLive) send(message string) {
	l.t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(message), &decoded); err != nil {
		l.t.Fatal(err)
	}
	l.server.toSend <- decoded
}

func (l *portprovgLive) settled(within time.Duration) bool {
	select {
	case <-l.done:
		return true
	case <-time.After(within):
		return false
	}
}

func (l *portprovgLive) finish() *provider.TranscriptionStreamPart {
	l.t.Helper()
	if !l.settled(2 * time.Second) {
		l.t.Fatal("stream did not finish")
	}
	l.partsMu.Lock()
	defer l.partsMu.Unlock()
	if l.err != nil {
		l.t.Fatalf("stream error: %v", l.err)
	}
	last := l.parts[len(l.parts)-1]
	if last.Type != provider.TranscriptionStreamPartTypeFinish {
		l.t.Fatalf("last part = %+v", last)
	}
	return last
}

func (l *portprovgLive) pushAudio(seconds int) {
	l.audio.chunks <- make([]byte, seconds*32000)
}

// closeAudio ends the input audio and waits for the end marker.
func (l *portprovgLive) closeAudio(marker string) {
	close(l.audio.chunks)
	l.server.waitFor(l.t, hasRealtimeInputKey(marker), time.Second)
}

func TestPortprovgLiveCompletesSegmentsOnGenerationCompleteAndVAD(t *testing.T) {
	l := portprovgStartLive(t, nil, 0)
	l.pushAudio(5)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"It was delivered into Earth orbit"}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"3s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if l.settled(100 * time.Millisecond) {
		t.Fatal("finished before the last segment")
	}
	// The server transcribes the rest and ends the speech at the end of the audio.
	l.send(`{"serverContent":{"inputTranscription":{"text":"on October 18th, 1989."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"5s"}}`)
	start := time.Now()
	finish := l.finish()
	if time.Since(start) > time.Second {
		t.Fatalf("finish took %s: the 3 s grace was waited", time.Since(start))
	}
	if finish.FinishText != "It was delivered into Earth orbit on October 18th, 1989." {
		t.Fatalf("text = %q", finish.FinishText)
	}
	var finals []string
	for _, part := range l.parts {
		if part.Type == provider.TranscriptionStreamPartTypeFinal {
			finals = append(finals, part.ID+"="+part.Text)
		}
	}
	if !reflect.DeepEqual(finals, []string{"google-segment-0=It was delivered into Earth orbit", "google-segment-1=on October 18th, 1989."}) {
		t.Fatalf("finals = %v", finals)
	}
}

func TestPortprovgLiveFallsBackToGraceWhenAudioEndsInSilence(t *testing.T) {
	l := portprovgStartLive(t, nil, 50*time.Millisecond)
	l.pushAudio(4)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"hello."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if got := l.finish().FinishText; got != "hello." {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveWaitsForBufferedAudioSegments(t *testing.T) {
	l := portprovgStartLive(t, nil, 0)
	l.pushAudio(2)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"hello."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"1s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if l.settled(150 * time.Millisecond) {
		t.Fatal("finished while the server was still processing buffered audio")
	}
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"1.200s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"world."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	if l.settled(150 * time.Millisecond) {
		t.Fatal("finished before the speech ended at the end of the audio")
	}
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	if got := l.finish().FinishText; got != "hello. world." {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveManualActivityDetection(t *testing.T) {
	options := map[string]interface{}{"google": map[string]interface{}{"streaming": map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}}}
	l := portprovgStartLive(t, options, 0)
	setupMsg := l.server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	wantConfig := map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}
	if got := setupMsg["setup"].(map[string]interface{})["realtimeInputConfig"]; !reflect.DeepEqual(got, wantConfig) {
		t.Fatalf("setup.realtimeInputConfig = %v", got)
	}
	if got := l.result.RequestBody.(map[string]interface{})["realtimeInputConfig"]; !reflect.DeepEqual(got, wantConfig) {
		t.Fatalf("request body realtimeInputConfig = %v", got)
	}

	l.pushAudio(3)
	l.closeAudio("activityEnd")
	sent := l.server.all()
	var kinds []string
	for _, message := range sent[1:] {
		input, _ := message["realtimeInput"].(map[string]interface{})
		for key := range input {
			kinds = append(kinds, key)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"activityStart", "audio", "activityEnd"}) {
		t.Fatalf("realtimeInput sequence = %v", kinds)
	}

	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"Book a venue. Then ask Sam."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"3s"}}`)
	finish := l.finish()
	if finish.FinishText != "Book a venue. Then ask Sam." {
		t.Fatalf("text = %q", finish.FinishText)
	}
	finals := 0
	for _, part := range l.parts {
		if part.Type == provider.TranscriptionStreamPartTypeFinal {
			finals++
		}
	}
	if finals != 1 {
		t.Fatalf("%d final segments", finals)
	}
}

func TestPortprovgLiveManualActivityWithoutSpeechFinishes(t *testing.T) {
	options := map[string]interface{}{"google": map[string]interface{}{"streaming": map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}}}
	l := portprovgStartLive(t, options, 0)
	l.pushAudio(3)
	l.closeAudio("activityEnd")
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0s"}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"3s"}}`)
	if got := l.finish().FinishText; got != "" {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveStreamingOptionsValidation(t *testing.T) {
	for name, streaming := range map[string]interface{}{
		"streaming not an object": "x",
		"detection not an object": map[string]interface{}{"automaticActivityDetection": true},
		"disabled not a boolean":  map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": "yes"}},
		"null streaming":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTranscriptionModelOptionsChecked(map[string]interface{}{"google": map[string]interface{}{"streaming": streaming}})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
	opts, err := parseTranscriptionModelOptionsChecked(map[string]interface{}{"google": map[string]interface{}{"streaming": map[string]interface{}{"automaticActivityDetection": map[string]interface{}{}}}})
	if err != nil || opts.ManualActivityDetection {
		t.Fatalf("opts = %+v err = %v", opts, err)
	}
}

// ---- Interactions file inspection (TS afb4e69924, google-file-inspection.test.ts
// and convert-to-google-interactions-input.test.ts) ----

const portprovgInspectionURI = "https://generativelanguage.googleapis.com/v1beta/files/inspection"

type portprovgInteractionsCase struct {
	name      string
	mediaType string
	options   map[string]interface{}
	expected  map[string]interface{}
}

func portprovgInteractionsCases() []portprovgInteractionsCase {
	m := func(kv ...interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	return []portprovgInteractionsCase{
		{"PDF pages", "application/pdf",
			m("processing", m("type", "static", "pageOptions", m("startPage", 3, "endPage", 4))),
			m("type", "document", "processing", m("type", "static", "page_options", m("start_page", 3, "end_page", 4)))},
		{"Word text layers", "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			m("processing", m("type", "static", "pageOptions", m("startPage", 2, "endPage", 3, "contentLayers", []interface{}{"page_text"}))),
			m("type", "document", "processing", m("type", "static", "page_options", m("start_page", 2, "end_page", 3, "content_layers", []interface{}{"page_text"})))},
		{"PowerPoint visual layers", "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			m("resolution", "high", "processing", m("type", "static", "pageOptions", m("startPage", 4, "endPage", 5, "contentLayers", []interface{}{"page_image"}))),
			m("type", "document", "resolution", "high", "processing", m("type", "static", "page_options", m("start_page", 4, "end_page", 5, "content_layers", []interface{}{"page_image"})))},
		{"spreadsheet cells and formulas", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			m("processing", m("type", "static", "spreadsheetOptions", m("sheetIndex", 3, "visibleCells", "A1:C5", "contentLayers", []interface{}{"data", "formulas"}))),
			m("type", "document", "processing", m("type", "static", "spreadsheet_options", m("sheet_index", 3, "visible_cells", "A1:C5", "content_layers", []interface{}{"data", "formulas"})))},
		{"OpenDocument sheet rendering", "application/vnd.oasis.opendocument.spreadsheet",
			m("resolution", "medium", "processing", m("type", "static", "spreadsheetOptions", m("sheetIndex", 1, "visibleCells", "B5", "contentLayers", []interface{}{"visual"}))),
			m("type", "document", "resolution", "medium", "processing", m("type", "static", "spreadsheet_options", m("sheet_index", 1, "visible_cells", "B5", "content_layers", []interface{}{"visual"})))},
		{"video frames", "video/mp4",
			m("processing", m("type", "static", "startOffset", 10, "endOffset", 20, "fps", 2, "contentLayers", []interface{}{"frames"})),
			m("type", "video", "processing", m("type", "static", "start_offset", "10s", "end_offset", "20s", "fps", 2, "content_layers", []interface{}{"frames"}))},
		{"audio clip starting at zero", "audio/mpeg",
			m("processing", m("type", "static", "startOffset", 0, "endOffset", 30.5)),
			m("type", "audio", "processing", m("type", "static", "start_offset", "0s", "end_offset", "30.5s"))},
	}
}

func portprovgInteractionsJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func portprovgConvertInteractions(t *testing.T, messages []types.Message, opts GoogleInteractionsProviderOptions) ([]map[string]interface{}, []types.Warning, error) {
	t.Helper()
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-4-hydrogen")
	steps, _, warnings, err := m.convertPrompt(types.Prompt{Messages: messages}, opts)
	return steps, warnings, err
}

func TestPortprovgInteractionsFileInspectionSerializes(t *testing.T) {
	type dataCase struct {
		name   string
		data   types.FileData
		inline bool
	}
	dataCases := []dataCase{
		{"bytes", types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}, true},
		{"base64", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, true},
		{"uploaded URL", types.FileData{Type: types.FileDataTypeURL, URL: portprovgInspectionURI}, false},
		{"provider reference", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": portprovgInspectionURI}}, false},
	}
	for _, toolResult := range []bool{false, true} {
		for _, dc := range dataCases {
			for _, ic := range portprovgInteractionsCases() {
				name := ic.name + "/" + dc.name
				if toolResult {
					name = "tool result/" + name
				}
				t.Run(name, func(t *testing.T) {
					options := map[string]interface{}{"google": ic.options}
					var messages []types.Message
					if toolResult {
						messages = []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
							ToolCallID: "call-1", ToolName: "view_file",
							Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
								types.FileContentBlock{FileData: dc.data, MediaType: ic.mediaType, ProviderOptions: options},
							}},
						}}}}
					} else {
						messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
							types.FileContent{FileData: dc.data, MediaType: ic.mediaType, ProviderOptions: options},
						}}}
					}
					steps, warnings, err := portprovgConvertInteractions(t, messages, GoogleInteractionsProviderOptions{})
					if err != nil {
						t.Fatal(err)
					}
					block := map[string]interface{}{"mime_type": ic.mediaType}
					for k, v := range ic.expected {
						block[k] = v
					}
					if dc.inline {
						block["data"] = "AQID"
					} else {
						block["uri"] = portprovgInspectionURI
					}
					var want interface{} = map[string]interface{}{"type": "user_input", "content": []interface{}{block}}
					if toolResult {
						want = map[string]interface{}{"type": "function_result", "call_id": "call-1", "name": "view_file", "result": []interface{}{block}}
					}
					if len(steps) != 1 || !reflect.DeepEqual(portprovgInteractionsJSON(t, steps[0]), portprovgInteractionsJSON(t, want)) {
						t.Fatalf("steps = %v\nwant   %v", portprovgInteractionsJSON(t, steps), portprovgInteractionsJSON(t, want))
					}
					if len(warnings) != 0 {
						t.Fatalf("warnings = %v", warnings)
					}
				})
			}
		}
	}
}

func TestPortprovgInteractionsProcessingKeyOrder(t *testing.T) {
	steps, _, err := portprovgConvertInteractions(t, []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
		MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/v.mp4"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processing": map[string]interface{}{
			"type": "static", "spreadsheetOptions": map[string]interface{}{"visibleCells": "A1", "sheetIndex": 2},
			"pageOptions": map[string]interface{}{"endPage": 4, "startPage": 3}, "contentLayers": []interface{}{"audio"}, "fps": 3, "endOffset": 9, "startOffset": 1,
		}}},
	}}}}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(steps)
	want := `"processing":{"type":"static","start_offset":"1s","end_offset":"9s","fps":3,"content_layers":["audio"],"page_options":{"start_page":3,"end_page":4},"spreadsheet_options":{"sheet_index":2,"visible_cells":"A1"}}`
	if !strings.Contains(string(raw), want) {
		t.Fatalf("steps = %s", raw)
	}
}

func TestPortprovgInteractionsSameWorkbookSelectionsStaySeparate(t *testing.T) {
	var parts []types.ContentPart
	for _, sheet := range []int{1, 3} {
		parts = append(parts, types.FileContent{
			MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": portprovgInspectionURI}},
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processing": map[string]interface{}{
				"type": "static", "spreadsheetOptions": map[string]interface{}{"sheetIndex": sheet},
			}}},
		})
	}
	steps, _, err := portprovgConvertInteractions(t, []types.Message{{Role: types.RoleUser, Content: parts}}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := steps[0]["content"].([]map[string]interface{})
	if len(steps) != 1 || len(content) != 2 {
		t.Fatalf("steps = %v", steps)
	}
	for i, sheet := range []float64{1, 3} {
		spreadsheet := portprovgInteractionsJSON(t, content[i]).(map[string]interface{})["processing"].(map[string]interface{})["spreadsheet_options"].(map[string]interface{})
		if spreadsheet["sheet_index"] != sheet {
			t.Fatalf("block %d = %v", i, content[i])
		}
	}
}

func TestPortprovgInteractionsPerFileResolutionBeatsCallDefault(t *testing.T) {
	file := func(options map[string]interface{}) []types.Message {
		return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
			MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, ProviderOptions: options,
		}}}}
	}
	steps, _, err := portprovgConvertInteractions(t, file(map[string]interface{}{"google": map[string]interface{}{"resolution": "high"}}), GoogleInteractionsProviderOptions{MediaResolution: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(steps); !strings.Contains(string(raw), `"resolution":"high"`) || strings.Contains(string(raw), `"low"`) {
		t.Fatalf("steps = %s", raw)
	}
	// The call-level default now reaches documents too, but never audio.
	steps, _, err = portprovgConvertInteractions(t, file(nil), GoogleInteractionsProviderOptions{MediaResolution: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(steps); !strings.Contains(string(raw), `"resolution":"low"`) {
		t.Fatalf("document steps = %s", raw)
	}
	audio := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{MediaType: "audio/mpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}}}}}
	steps, _, err = portprovgConvertInteractions(t, audio, GoogleInteractionsProviderOptions{MediaResolution: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(steps); strings.Contains(string(raw), "resolution") {
		t.Fatalf("audio steps = %s", raw)
	}
}

func TestPortprovgInteractionsRejectsMalformedInspectionOptions(t *testing.T) {
	for name, options := range map[string]map[string]interface{}{
		"ultra_high resolution":          {"resolution": "ultra_high"},
		"processing without a type":      {"processing": map[string]interface{}{"pageOptions": map[string]interface{}{"startPage": 3}}},
		"spreadsheetOptions as an array": {"processing": map[string]interface{}{"type": "static", "spreadsheetOptions": []interface{}{map[string]interface{}{"sheetIndex": 1}}}},
		"start page zero":                {"processing": map[string]interface{}{"type": "static", "pageOptions": map[string]interface{}{"startPage": 0}}},
		"negative offset":                {"processing": map[string]interface{}{"type": "static", "startOffset": -1}},
		"fps above 24":                   {"processing": map[string]interface{}{"type": "static", "fps": 25}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := portprovgConvertInteractions(t, []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
				MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"},
				ProviderOptions: map[string]interface{}{"google": options},
			}}}}, GoogleInteractionsProviderOptions{})
			var validation *providererrors.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
}

// TS convert-to-google-interactions-input.test.ts "tool-result messages":
// results are top-level function_result inputs and a document nests in its
// result without a warning.
func TestPortprovgInteractionsToolResultsAreTopLevel(t *testing.T) {
	call := types.ToolCallContent{ToolCallID: "call_abc", ToolName: "getWeather", Input: `{"location":"NYC"}`}
	result := func(output types.ToolResultOutput) []types.Message {
		return []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{call}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call_abc", ToolName: "getWeather", Output: &output}}},
		}
	}
	steps, warnings, err := portprovgConvertInteractions(t, result(types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "It is sunny."}), GoogleInteractionsProviderOptions{})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err = %v warnings = %v", err, warnings)
	}
	want := map[string]interface{}{"type": "function_result", "call_id": "call_abc", "name": "getWeather", "result": "It is sunny."}
	if len(steps) != 3 || !reflect.DeepEqual(portprovgInteractionsJSON(t, steps[2]), portprovgInteractionsJSON(t, want)) {
		t.Fatalf("steps = %v", portprovgInteractionsJSON(t, steps))
	}

	steps, warnings, err = portprovgConvertInteractions(t, result(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}},
	}}), GoogleInteractionsProviderOptions{})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err = %v warnings = %v", err, warnings)
	}
	want = map[string]interface{}{"type": "function_result", "call_id": "call_abc", "name": "getWeather", "result": []interface{}{
		map[string]interface{}{"type": "document", "data": "AQID", "mime_type": "application/pdf"},
	}}
	if !reflect.DeepEqual(portprovgInteractionsJSON(t, steps[2]), portprovgInteractionsJSON(t, want)) {
		t.Fatalf("step = %v", portprovgInteractionsJSON(t, steps[2]))
	}
}

// TS "keeps mixed file content inside its tool result (compact: %s)".
func TestPortprovgInteractionsMixedToolResultFiles(t *testing.T) {
	fileURI := portprovgInspectionURI
	firstCall := types.ToolCallContent{ToolCallID: "first-call", ToolName: "viewFile", Input: `{}`, ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"interactionId": "stored-interaction"}}}
	secondCall := types.ToolCallContent{ToolCallID: "second-call", ToolName: "viewFile", Input: `{}`}
	static := func(extra map[string]interface{}) map[string]interface{} {
		processing := map[string]interface{}{"type": "static"}
		for k, v := range extra {
			processing[k] = v
		}
		return processing
	}
	firstOutput := types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.TextContentBlock{Text: "First selection"},
		types.FileContentBlock{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"},
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"resolution": "high", "processing": static(map[string]interface{}{"pageOptions": map[string]interface{}{"startPage": 1, "endPage": 1}})}}},
		types.FileContentBlock{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "BAUG"}},
		types.FileContentBlock{MediaType: "audio/mpeg", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": fileURI}},
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processing": static(map[string]interface{}{"startOffset": 0, "endOffset": 10})}}},
		types.FileContentBlock{MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeURL, URL: fileURI}},
	}}
	secondOutput := types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": fileURI}},
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processing": static(map[string]interface{}{"pageOptions": map[string]interface{}{"startPage": 2, "endPage": 2}})}}},
	}}
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "full history", true: "compact"}[compact], func(t *testing.T) {
			messages := []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{firstCall}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{secondCall}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{ToolCallID: "first-call", ToolName: "viewFile", Output: &firstOutput, ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"signature": "first-signature"}}},
					types.ToolResultContent{ToolCallID: "second-call", ToolName: "viewFile", Output: &secondOutput, ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"signature": "second-signature"}}},
				}},
			}
			opts := GoogleInteractionsProviderOptions{}
			if compact {
				opts.PreviousInteractionID = "stored-interaction"
			}
			steps, warnings, err := portprovgConvertInteractions(t, messages, opts)
			if err != nil || len(warnings) != 0 {
				t.Fatalf("err = %v warnings = %v", err, warnings)
			}
			var results []interface{}
			for _, step := range steps {
				if step["type"] == "function_result" {
					results = append(results, portprovgInteractionsJSON(t, step))
				}
			}
			first := map[string]interface{}{"type": "function_result", "call_id": "first-call", "name": "viewFile", "signature": "first-signature", "result": []interface{}{
				map[string]interface{}{"type": "text", "text": "First selection"},
				map[string]interface{}{"type": "document", "data": "AQID", "mime_type": "application/pdf", "resolution": "high", "processing": map[string]interface{}{"type": "static", "page_options": map[string]interface{}{"start_page": 1, "end_page": 1}}},
				map[string]interface{}{"type": "image", "data": "BAUG", "mime_type": "image/png"},
				map[string]interface{}{"type": "audio", "uri": fileURI, "mime_type": "audio/mpeg", "processing": map[string]interface{}{"type": "static", "start_offset": "0s", "end_offset": "10s"}},
				map[string]interface{}{"type": "video", "uri": fileURI, "mime_type": "video/mp4"},
			}}
			second := map[string]interface{}{"type": "function_result", "call_id": "second-call", "name": "viewFile", "signature": "second-signature", "result": []interface{}{
				map[string]interface{}{"type": "document", "uri": fileURI, "mime_type": "application/pdf", "processing": map[string]interface{}{"type": "static", "page_options": map[string]interface{}{"start_page": 2, "end_page": 2}}},
			}}
			want := []interface{}{first, second}
			if compact {
				want = []interface{}{second}
			}
			if !reflect.DeepEqual(results, portprovgInteractionsJSON(t, want)) {
				t.Fatalf("results = %v\nwant     %v", results, want)
			}
		})
	}
}

// supportedUrls: uploaded Gemini Files API URLs are accepted for any media type.
func TestPortprovgInteractionsSupportsFilesAPIURLs(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-4-hydrogen")
	patterns := m.SupportedURLs()["*"]
	if len(patterns) != 1 || !regexp.MustCompile(patterns[0]).MatchString(portprovgInspectionURI) {
		t.Fatalf("patterns = %v", patterns)
	}
}
