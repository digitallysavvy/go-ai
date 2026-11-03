package openai

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r85provbCapabilitiesTS is the TS getOpenAILanguageModelCapabilities shape.
type r85provbCapabilitiesTS struct {
	IsReasoningModel                       bool     `json:"isReasoningModel"`
	SystemMessageMode                      string   `json:"systemMessageMode"`
	SupportsFlexProcessing                 bool     `json:"supportsFlexProcessing"`
	SupportsPriorityProcessing             bool     `json:"supportsPriorityProcessing"`
	SupportsConfigurationUpdate            bool     `json:"supportsConfigurationUpdate"`
	SupportsAsyncToolCalling               bool     `json:"supportsAsyncToolCalling"`
	SupportedReasoningEfforts              []string `json:"supportedReasoningEfforts"`
	SupportsChatFunctionToolsWithReasoning bool     `json:"supportsChatFunctionToolsWithReasoning"`
	SupportsNonReasoningParameters         bool     `json:"supportsNonReasoningParameters"`
}

// TestR85provbCapabilitiesMatchNode compares GetLanguageModelCapabilities
// with TS getOpenAILanguageModelCapabilities (openai-model-catalog.ts at
// 30548ee7f9, run under Node 24) for every Go model ID constant plus edge
// IDs (dated GPT-5 variants, pro/codex/chat, GPT-6.1 Terra, unknown IDs).
// TS d07b475bc8 (GPT-5.x efforts, chat function tools) and 01d7a03b05
// (GPT-6.1 Terra).
func TestR85provbCapabilitiesMatchNode(t *testing.T) {
	f, err := os.Open("testdata/tscases/r85_capabilities_node.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]r85provbCapabilitiesTS
	if err := json.NewDecoder(zr).Decode(&want); err != nil {
		t.Fatal(err)
	}
	if len(want) < 100 {
		t.Fatalf("only %d recorded IDs", len(want))
	}
	for id, w := range want {
		c := GetLanguageModelCapabilities(id)
		got := r85provbCapabilitiesTS{
			IsReasoningModel:                       c.IsReasoningModel,
			SystemMessageMode:                      c.SystemMessageMode,
			SupportsFlexProcessing:                 c.SupportsFlexProcessing,
			SupportsPriorityProcessing:             c.SupportsPriorityProcessing,
			SupportsConfigurationUpdate:            c.SupportsConfigurationUpdate,
			SupportsAsyncToolCalling:               c.SupportsAsyncToolCalling,
			SupportedReasoningEfforts:              c.SupportedReasoningEfforts,
			SupportsChatFunctionToolsWithReasoning: c.SupportsChatFunctionToolsWithReasoning,
			SupportsNonReasoningParameters:         c.SupportsNonReasoningParameters,
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s:\n got  %+v\n want %+v", id, got, w)
		}
	}
}

func r85provbChatBody(t *testing.T, modelID string, opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning) {
	t.Helper()
	m := NewLanguageModel(New(Config{APIKey: "k"}), modelID)
	if opts.Prompt.Text == "" && len(opts.Prompt.Messages) == 0 {
		opts.Prompt = types.Prompt{Text: "hi"}
	}
	body, warnings, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	return body, warnings
}

func r85provbLevel(l types.ReasoningLevel) *types.ReasoningLevel { return &l }

// TS d07b475bc8 chat tests: unsupported top-level reasoning rounds to a
// supported effort; Chat never sends "max"; GPT-5.6 with function tools
// sends "none"; GPT-5 keeps prompt_cache_retention.
func TestR85provbChatReasoningEfforts(t *testing.T) {
	fnTools := []types.Tool{{Type: types.ToolTypeFunction, Name: "test-tool", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}}
	compat := func(from, to string) []types.Warning {
		return []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: `reasoning "` + from + `" is not directly supported by this model. mapped to effort "` + to + `".`}}
	}
	toolsWarning := []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: `gpt-5.6-luna does not support function tools with reasoning in the Chat Completions API. reasoning effort set to "none". Use the Responses API to combine reasoning with function tools.`}}
	tests := []struct {
		name     string
		model    string
		opts     provider.GenerateOptions
		effort   interface{}
		warnings []types.Warning
	}{
		{"gpt-5.5 minimal->low", "gpt-5.5", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMinimal)}, "low", compat("minimal", "low")},
		{"gpt-6-sol max->xhigh", "gpt-6-sol", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "xhigh", compat("max", "xhigh")},
		{"gpt-5.6 max->xhigh on chat", "gpt-5.6", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "xhigh", compat("max", "xhigh")},
		{"gpt-5.1 xhigh->high", "gpt-5.1", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningXHigh)}, "high", compat("xhigh", "high")},
		{"gpt-5-nano none->minimal", "gpt-5-nano", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningNone)}, "minimal", compat("none", "minimal")},
		{"unknown id sends max", "my-model", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "max", nil},
		{"gpt-5.6-luna tools low->none", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningLow), Tools: fnTools}, "none", toolsWarning},
		{"gpt-5.6-luna tools none", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningNone), Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools default", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningDefault), Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools unset", "gpt-5.6-luna", provider.GenerateOptions{Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools provider effort", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMedium), Tools: fnTools, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "low"}}}, "low", nil},
		{"gpt-5.6-luna no tools", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningLow)}, "low", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			body, warnings := r85provbChatBody(t, tt.model, &opts)
			if body["reasoning_effort"] != tt.effort {
				t.Errorf("reasoning_effort = %#v, want %#v", body["reasoning_effort"], tt.effort)
			}
			if !reflect.DeepEqual(warnings, tt.warnings) {
				t.Errorf("warnings = %#v, want %#v", warnings, tt.warnings)
			}
		})
	}

	body, warnings := r85provbChatBody(t, "gpt-5.5", &provider.GenerateOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheRetention": "24h"}}})
	if body["prompt_cache_retention"] != "24h" || len(warnings) != 0 {
		t.Errorf("gpt-5.5 prompt_cache_retention = %#v, warnings %#v", body["prompt_cache_retention"], warnings)
	}
}

// TS d07b475bc8 responses tests: GPT-5 keeps logprobs and
// prompt_cache_retention; GPT-5.6 sends max.
func TestR85provbResponsesReasoningEfforts(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5.5")
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "low", "logprobs": 5, "promptCacheRetention": "24h"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["top_logprobs"] != 5 || body["prompt_cache_retention"] != "24h" || len(warnings) != 0 {
		t.Errorf("gpt-5.5: top_logprobs %#v, prompt_cache_retention %#v, include %#v, warnings %#v", body["top_logprobs"], body["prompt_cache_retention"], body["include"], warnings)
	}

	m = NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5.6-luna")
	body, _, warnings, err = m.buildRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: r85provbLevel(types.ReasoningMax)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := body["reasoning"].(map[string]interface{}); r["effort"] != "max" || len(warnings) != 0 {
		t.Errorf("gpt-5.6-luna: reasoning %#v, warnings %#v", body["reasoning"], warnings)
	}
}

// TS 8375141ef4 / 226b7daf82: streaming transcription with
// gpt-live-transcribe and gpt-transcribe (languages list, no delay for
// gpt-transcribe, REST-only languages/keywords warnings); REST rejects
// gpt-live-transcribe.
func TestR85provbStreamingTranscriptionModels(t *testing.T) {
	for id, want := range map[string]bool{
		"gpt-realtime-whisper": true, "gpt-realtime-whisper-2026-01-01": true,
		"gpt-live-transcribe": true, "gpt-live-transcribe-2026-10-01": true,
		"gpt-transcribe": true, "gpt-transcribe-2026-10-01": true,
		"whisper-1": false, "gpt-4o-transcribe": false, "custom": false, "gpt-transcriber": false,
	} {
		if got := SupportsStreamingTranscription(id); got != want {
			t.Errorf("SupportsStreamingTranscription(%q) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{
		"gpt-realtime-whisper": true, "gpt-live-transcribe": true, "gpt-live-transcribe-x": true,
		"gpt-transcribe": false, "whisper-1": false, "custom": false,
	} {
		if got := IsRealtimeTranscriptionModelID(id); got != want {
			t.Errorf("IsRealtimeTranscriptionModelID(%q) = %v, want %v", id, got, want)
		}
	}

	_, err := NewTranscriptionModel(New(Config{APIKey: "k"}), "gpt-live-transcribe").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "non-streaming transcription with gpt-live-transcribe" {
		t.Fatalf("DoTranscribe(gpt-live-transcribe) err = %v", err)
	}

	po := map[string]interface{}{"openai": map[string]interface{}{
		"language": "en", "languages": []interface{}{"en"}, "keywords": []interface{}{"AI SDK"},
		"streaming": map[string]interface{}{"delay": "low"},
	}}
	opts := TranscriptionStreamOpenAIOptions(po)
	format := provider.AudioFormat{Type: "audio/pcm"}

	transcription := func(session map[string]interface{}) map[string]interface{} {
		return session["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})
	}
	if got := transcription(BuildOpenAIRealtimeTranscriptionSession("gpt-transcribe", format, opts)); !reflect.DeepEqual(got, map[string]interface{}{"model": "gpt-transcribe", "languages": []string{"en"}}) {
		t.Errorf("gpt-transcribe transcription = %#v", got)
	}
	if got := transcription(BuildOpenAIRealtimeTranscriptionSession("gpt-live-transcribe", format, opts)); !reflect.DeepEqual(got, map[string]interface{}{"model": "gpt-live-transcribe", "languages": []string{"en"}, "delay": "low"}) {
		t.Errorf("gpt-live-transcribe transcription = %#v", got)
	}
	if got := transcription(BuildOpenAIRealtimeTranscriptionSession("gpt-realtime-whisper", format, opts)); !reflect.DeepEqual(got, map[string]interface{}{"model": "gpt-realtime-whisper", "language": "en", "delay": "low"}) {
		t.Errorf("gpt-realtime-whisper transcription = %#v", got)
	}

	rest := func(option string) types.Warning {
		return types.Warning{Type: "unsupported", Feature: "providerOptions.openai." + option, Details: "OpenAI streaming transcription does not support " + option + "."}
	}
	want := []types.Warning{rest("languages"), rest("keywords"), {Type: "unsupported", Feature: "providerOptions.openai.streaming.delay", Details: "OpenAI streaming transcription with gpt-transcribe does not support delay."}}
	if got := StreamingTranscriptionWarnings("gpt-transcribe", opts); !reflect.DeepEqual(got, want) {
		t.Errorf("gpt-transcribe warnings = %#v", got)
	}
	if got := StreamingTranscriptionWarnings("gpt-live-transcribe", opts); !reflect.DeepEqual(got, want[:2]) {
		t.Errorf("gpt-live-transcribe warnings = %#v", got)
	}
}

// TS 436374eb36 mapWebSearchResult: every status but completed is an error.
func TestR85provbUnfinishedWebSearchIsError(t *testing.T) {
	for status, want := range map[string]bool{"completed": false, "searching": true, "in_progress": true, "failed": true, "incomplete": true} {
		if got := isFailedWebSearchStatus(status); got != want {
			t.Errorf("isFailedWebSearchStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

// TS 47b1d8b2f7 isInputJsonData: objects and non-empty arrays of objects
// within depth 32, 10,000 values and 256 KiB go as input_json.
func TestR85provbDecisionInputJSON(t *testing.T) {
	nested := func(levels int) interface{} {
		var v interface{} = map[string]interface{}{"leaf": true}
		for i := 1; i < levels; i++ {
			v = map[string]interface{}{"next": v}
		}
		return v
	}
	withKeys := func(n int) interface{} {
		m := map[string]interface{}{}
		for i := 0; i < n; i++ {
			m["k"+strconv.Itoa(i)] = i
		}
		return m
	}
	big := map[string]interface{}{"text": strings.Repeat("x", 200*1024)}
	tooBig := map[string]interface{}{"text": strings.Repeat("é", 129*1024)} // 258 KiB of UTF-8
	tests := []struct {
		name   string
		value  interface{}
		native bool
	}{
		{"object", map[string]interface{}{"a": 1}, true},
		{"array of objects", []interface{}{map[string]interface{}{"charge": 49}, map[string]interface{}{"charge": 49}}, true},
		{"empty array", []interface{}{}, false},
		{"mixed array", []interface{}{map[string]interface{}{"a": 1}, 2}, false},
		{"string", "hi", false},
		{"number", 3, false},
		{"null", nil, false},
		{"32 container levels", nested(32), true},
		{"33 container levels", nested(33), false},
		{"9,000 values", withKeys(9000), true},
		{"10,000 values", withKeys(10000), false}, // the object itself is one more value
		{"200 KiB of JSON", big, true},
		{"over 256 KiB of UTF-8", tooBig, false},
	}
	for _, tt := range tests {
		if got := decisionIsInputJSONData(decisionJSONText(tt.value)); got != tt.native {
			t.Errorf("%s: native = %v, want %v", tt.name, got, tt.native)
		}
	}

	opts := map[string]interface{}{"openai": map[string]interface{}{"promptCacheKey": "k1", "safetyIdentifier": "u", "other": 1}}
	parsed, err := parseDecisionProviderOptions(opts)
	if err != nil || parsed.PromptCacheKey == nil || *parsed.PromptCacheKey != "k1" {
		t.Fatalf("parsed = %+v, err %v", parsed, err)
	}
	if w := decisionUnsupportedOptionWarnings(opts); len(w) != 1 || w[0].Feature != "providerOptions.openai.other" {
		t.Fatalf("warnings = %#v", w)
	}
	if _, err := parseDecisionProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"promptCacheKey": 1}}); err == nil {
		t.Fatal("a non-string promptCacheKey must fail")
	}
}

// TestR85provbAPIKeyFunc: TS 7716e189ee async credentials. The APIKeyFunc is
// called for every request and its key is sent; the environment is not
// read; its error fails the request; APIKey plus APIKeyFunc is an
// InvalidArgumentError.
func TestR85provbAPIKeyFunc(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
	}))
	defer srv.Close()

	calls := 0
	keyFunc := func(ctx context.Context) (string, error) {
		calls++
		return fmt.Sprintf("k%d", calls), nil
	}
	call := func(cfg Config) error {
		model, err := New(cfg).LanguageModel("gpt-4o")
		if err != nil {
			return err
		}
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return err
	}
	cfg := Config{BaseURL: srv.URL}
	cfg.APIKeyFunc = keyFunc
	_ = call(cfg)
	_ = call(cfg)
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "Bearer k1" || got[1] != "Bearer k2" {
		t.Fatalf("credential headers = %q, want Bearer k1, Bearer k2", got)
	}

	sentinel := errors.New("vault unavailable")
	cfg.APIKeyFunc = func(context.Context) (string, error) { return "", sentinel }
	if err := call(cfg); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the APIKeyFunc error", err)
	}

	cfg.APIKeyFunc = keyFunc
	cfg.APIKey = "static"
	err := call(cfg)
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("both set: err = %v, want InvalidArgumentError", err)
	}
}

// R85 reviewer item 3: a 2xx JSON body read without DoJSON is decoded like
// TS response.text() (a leading BOM is dropped) before it is parsed.
func TestR85provbTranscriptionBOMBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"text\":\"hi\",\"usage\":{\"type\":\"duration\",\"seconds\":3}}"))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-transcribe")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi" || res.DurationInSeconds == nil || *res.DurationInSeconds != 3 {
		t.Fatalf("result = %+v", res)
	}
}
