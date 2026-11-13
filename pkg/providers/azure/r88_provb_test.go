package azure

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// r88provbRequest is one request a test server received.
type r88provbRequest struct {
	path  string
	json  map[string]interface{}
	forms map[string]string
}

// r88provbServer answers every request with body and records it.
func r88provbServer(t *testing.T, contentType, body string) (*httptest.Server, *[]r88provbRequest) {
	t.Helper()
	var reqs []r88provbRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := r88provbRequest{path: r.URL.Path, forms: map[string]string{}}
		raw, _ := io.ReadAll(r.Body)
		if mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); strings.HasPrefix(mt, "multipart/") {
			mr := multipart.NewReader(strings.NewReader(string(raw)), params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				v, _ := io.ReadAll(part)
				rec.forms[part.FormName()] = string(v)
			}
		} else {
			_ = json.Unmarshal(raw, &rec.json)
		}
		reqs = append(reqs, rec)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// TS 6400cf8b88 (azure-openai-provider.test.ts): Azure chat merges openai
// and azure options, azure winning.
func TestR88provbAzureChatOptionNamespaces(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.ChatModel("test-deployment")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		opts map[string]interface{}
		want map[string]interface{}
	}{
		{map[string]interface{}{"azure": map[string]interface{}{"user": "azure-user"}}, map[string]interface{}{"user": "azure-user"}},
		{map[string]interface{}{
			"openai": map[string]interface{}{"user": "openai-user", "logitBias": map[string]interface{}{"50256": -100}},
			"azure":  map[string]interface{}{"user": "azure-user"},
		}, map[string]interface{}{"user": "azure-user", "logit_bias": map[string]interface{}{"50256": float64(-100)}}},
	} {
		*reqs = nil
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, ProviderOptions: tc.opts}); err != nil {
			t.Fatal(err)
		}
		for k, v := range tc.want {
			if !reflect.DeepEqual((*reqs)[0].json[k], v) {
				t.Errorf("%v: %s = %v, want %v", tc.opts, k, (*reqs)[0].json[k], v)
			}
		}
	}
	// An invalid azure option fails like an invalid openai one.
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"user": 5}}})
	if err == nil || !strings.Contains(err.Error(), "invalid azure provider options") {
		t.Fatalf("err = %v, want invalid azure provider options", err)
	}
}

// TS 6400cf8b88 (azure-transcription-model.test.ts): OpenAI transcription
// options under azure reach the OpenAI API (azure winning, no MAI language
// warning) and warn on the Azure Speech API.
func TestR88provbAzureTranscriptionOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"text":"hello","language":"en","duration":1,"combinedPhrases":[{"text":"hello"}],"phrases":[]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, ResourceName: "r", SpeechBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	whisper, _ := p.TranscriptionModel("whisper-1")
	audio := &provider.TranscriptionOptions{Audio: []byte{0xff, 0xf3, 0x44, 0xc4}, MimeType: "audio/mpeg"}

	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"language": "en", "prompt": "Names"}}
	res, err := whisper.DoTranscribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if f := (*reqs)[0].forms; f["language"] != "en" || f["prompt"] != "Names" {
		t.Errorf("form = %v, want language en, prompt Names", f)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}

	*reqs = nil
	audio.ProviderOptions = map[string]interface{}{
		"openai": map[string]interface{}{"language": "de", "prompt": "Names"},
		"azure":  map[string]interface{}{"language": "en"},
	}
	if _, err := whisper.DoTranscribe(context.Background(), audio); err != nil {
		t.Fatal(err)
	}
	if f := (*reqs)[0].forms; f["language"] != "en" || f["prompt"] != "Names" {
		t.Errorf("form = %v, want language en, prompt Names", f)
	}

	*reqs = nil
	mai, _ := p.TranscriptionModel("mai-transcribe-2")
	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"prompt": "Names"}}
	res, err = mai.DoTranscribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*reqs)[0].path, "/speechtotext/") {
		t.Errorf("path = %s, want the Speech API", (*reqs)[0].path)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "providerOptions.azure.prompt", Details: "This option requires the OpenAI transcription API."}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %+v, want %+v", res.Warnings, want)
	}

	// The OpenAI options are validated under azure too.
	audio.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"temperature": 5}}
	if _, err := whisper.DoTranscribe(context.Background(), audio); err == nil {
		t.Fatal("want a validation error for azure.temperature 5")
	}
}

// TS 6400cf8b88 (azure-speech-model.test.ts).
func TestR88provbAzureSpeechOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "audio/mpeg", "ID3")
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, ResourceName: "r", SpeechBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	tts, _ := p.SpeechModel("tts-1")
	res, err := tts.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", ProviderOptions: map[string]interface{}{
		"openai": map[string]interface{}{"speed": 2},
		"azure":  map[string]interface{}{"instructions": "Speak calmly", "speed": 1.5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if b := (*reqs)[0].json; b["instructions"] != "Speak calmly" || b["speed"] != 1.5 {
		t.Errorf("body = %v", b)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}

	voice, _ := p.SpeechModel("mai-voice-2")
	res, err = voice.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", Voice: "en-US-Jasper:MAI-Voice-2", ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"speed": 1.5}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w == (types.Warning{Type: "unsupported", Feature: "providerOptions.azure.speed", Details: "This option requires the OpenAI speech API."})
	}
	if !found {
		t.Errorf("warnings = %+v, want the OpenAI speech API warning", res.Warnings)
	}
}

// TS 6400cf8b88 (azure-mai-image-model.test.ts "accepts OpenAI options under
// the azure key on the OpenAI API").
func TestR88provbAzureImageOpenAIOptions(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"data":[{"b64_json":"aGk="}]}`)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.ImageModel("gpt-image-1")
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", ProviderOptions: map[string]interface{}{
		"openai": map[string]interface{}{"quality": "low", "background": "opaque"},
		"azure":  map[string]interface{}{"quality": "high"},
	}}); err != nil {
		t.Fatal(err)
	}
	if b := (*reqs)[0].json; b["quality"] != "high" || b["background"] != "opaque" {
		t.Errorf("body = %v", b)
	}
}

// TS 6400cf8b88 (azure-mai-image-model.test.ts "accepts OpenAI image edit
// options under the azure key %s").
func TestR88provbAzureImageEditOpenAIOptions(t *testing.T) {
	for name, openaiOpts := range map[string]map[string]interface{}{
		"alone":                     nil,
		"overriding openai options": {"inputFidelity": "low", "markup": "data:image/png;base64,AQID", "background": "opaque"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, reqs := r88provbServer(t, "application/json", `{"data":[{"b64_json":"aGk="}]}`)
			p, err := New(Config{APIKey: "k", ResourceName: "test-resource", BaseURL: srv.URL + "/openai"})
			if err != nil {
				t.Fatal(err)
			}
			m, _ := p.ImageModel("gpt-image-2.6-flare")
			po := map[string]interface{}{"azure": map[string]interface{}{"inputFidelity": "high", "markup": "data:image/png;base64,iVBORw=="}}
			if openaiOpts != nil {
				po["openai"] = openaiOpts
			}
			res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p",
				Files:           []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte{137, 80, 78, 71}}},
				ProviderOptions: po})
			if err != nil {
				t.Fatal(err)
			}
			f := (*reqs)[0].forms
			if !strings.HasSuffix((*reqs)[0].path, "/images/edits") {
				t.Errorf("path = %s", (*reqs)[0].path)
			}
			if f["input_fidelity"] != "high" || f["markup"] != string([]byte{137, 80, 78, 71}) {
				t.Errorf("form = %v", f)
			}
			wantBackground := ""
			if openaiOpts != nil {
				wantBackground = "opaque"
			}
			if f["background"] != wantBackground {
				t.Errorf("background = %q, want %q", f["background"], wantBackground)
			}
			if len(res.Warnings) != 0 {
				t.Errorf("warnings = %v", res.Warnings)
			}
		})
	}
}

// TS 27528dbf24 / bf10079bc2 / 5cc6d284e8 (azure-openai-provider.test.ts
// "custom tool", "apply_patch" and "shell" blocks): the Azure tool
// factories go through the shared Responses path with Azure item metadata.
func TestR88provbAzureResponsesToolFactories(t *testing.T) {
	sql := `SELECT "name" FROM users WHERE age > 25`
	syntax, def := "regex", "SELECT .+"
	custom := NewCustomTool(openaitool.WithDescription("Write a SQL query."), openaitool.WithFormat(openaitool.CustomToolFormat{Type: "grammar", Syntax: &syntax, Definition: &def})).ToTool("write_sql")
	ref := "cntr_test"
	for _, tc := range []struct {
		name     string
		tool     types.Tool
		output   string
		wantTool string
		wantName string
		wantArgs string
		executed bool
	}{
		{"custom", custom,
			`{"type":"custom_tool_call","id":"ct_sql","call_id":"call_sql","name":"write_sql","input":` + r88provbQuote(sql) + `,"status":"completed"}`,
			`{"description":"Write a SQL query.","format":{"definition":"SELECT .+","syntax":"regex","type":"grammar"},"name":"write_sql","type":"custom"}`,
			"write_sql", r88provbQuote(sql), false},
		{"apply_patch", ApplyPatch(),
			`{"type":"apply_patch_call","id":"apc_1","call_id":"call_patch","status":"completed","operation":{"type":"delete_file","path":"a.txt"}}`,
			`{"type":"apply_patch"}`, "openai.apply_patch", `{"callId":"call_patch","operation":{"type":"delete_file","path":"a.txt"}}`, false},
		{"shell local", Shell(responses.WithShellEnvironment(responses.ShellEnvironment{Type: "local", Skills: []responses.ShellSkill{{Name: r88provbStr("test"), Description: r88provbStr("Test"), Path: r88provbStr("/skills/test")}}})),
			`{"type":"shell_call","id":"sh_1","call_id":"call_shell","status":"completed","action":{"commands":["ls"]}}`,
			`{"environment":{"skills":[{"description":"Test","name":"test","path":"/skills/test"}],"type":"local"},"type":"shell"}`, "openai.shell", "", false},
		{"shell container_auto", Shell(responses.WithShellEnvironment(responses.ShellEnvironment{Type: "container_auto"})),
			`{"type":"shell_call","id":"sh_1","call_id":"call_shell","status":"completed","action":{"commands":["ls"]}}`,
			`{"environment":{"type":"container_auto"},"type":"shell"}`, "openai.shell", "", true},
		{"shell container_reference", Shell(responses.WithShellEnvironment(responses.ShellEnvironment{Type: "container_reference", ContainerID: &ref})),
			`{"type":"shell_call","id":"sh_1","call_id":"call_shell","status":"completed","action":{"commands":["ls"]}}`,
			`{"environment":{"container_id":"cntr_test","type":"container_reference"},"type":"shell"}`, "openai.shell", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := r88provbServer(t, "application/json", `{"id":"resp_1","created_at":1741257730,"model":"test-deployment","output":[`+tc.output+`],"usage":{"input_tokens":2,"output_tokens":4}}`)
			p, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/openai"})
			if err != nil {
				t.Fatal(err)
			}
			m, _ := p.ResponsesModel("test-deployment")
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, Tools: []types.Tool{tc.tool}})
			if err != nil {
				t.Fatal(err)
			}
			tools, _ := json.Marshal((*reqs)[0].json["tools"])
			if string(tools) != "["+tc.wantTool+"]" {
				t.Errorf("tools = %s, want [%s]", tools, tc.wantTool)
			}
			if len(res.ToolCalls) != 1 {
				t.Fatalf("tool calls = %+v", res.ToolCalls)
			}
			call := res.ToolCalls[0]
			meta, _ := call.ProviderMetadata["azure"].(map[string]interface{})
			if meta["itemId"] == nil {
				t.Errorf("tool call metadata = %v, want azure.itemId", call.ProviderMetadata)
			}
			if tc.wantArgs != "" && call.RawArguments != tc.wantArgs {
				t.Errorf("input = %s, want %s", call.RawArguments, tc.wantArgs)
			}
			if call.ProviderExecuted != tc.executed {
				t.Errorf("providerExecuted = %v, want %v", call.ProviderExecuted, tc.executed)
			}
		})
	}
}

func r88provbQuote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func r88provbStr(s string) *string { return &s }

// TS 41465fd1b6 (azure-transcription-model.test.ts): shared timestamp
// granularities map onto Azure Speech timestamps, with the override and
// unsupported-model warnings (it.each cases the recorder cannot replay).
func TestR88provbAzureSpeechTimestampGranularities(t *testing.T) {
	srv, reqs := r88provbServer(t, "application/json", `{"durationMilliseconds":1000,"combinedPhrases":[{"text":"Hello"}],"phrases":[{"text":"Hello","offsetMilliseconds":40,"durationMilliseconds":400,"locale":"en-US","words":[{"text":"Hello","offsetMilliseconds":40,"durationMilliseconds":400}]}]}`)
	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	run := func(model string, granularities []string, timestamps string) (*types.TranscriptionResult, interface{}) {
		t.Helper()
		*reqs = nil
		m, _ := p.TranscriptionModel(model)
		opts := &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav", TimestampGranularities: granularities}
		if timestamps != "" {
			opts.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"timestamps": timestamps}}
		}
		res, err := m.DoTranscribe(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		var def struct {
			EnhancedMode struct {
				ModelOptions map[string]interface{} `json:"modelOptions"`
			} `json:"enhancedMode"`
		}
		_ = json.Unmarshal([]byte((*reqs)[0].forms["definition"]), &def)
		return res, def.EnhancedMode.ModelOptions["timestamps"]
	}

	res, ts := run("mai-transcribe-2", []string{"word", "segment"}, "")
	if ts != "word" || len(res.Warnings) != 0 {
		t.Errorf("word+segment: timestamps %v, warnings %v", ts, res.Warnings)
	}
	if want := []types.TranscriptionWord{{Text: "Hello", StartSecond: 0.04, EndSecond: 0.44}}; !reflect.DeepEqual(res.Words, want) {
		t.Errorf("words = %+v, want %+v", res.Words, want)
	}
	if res, ts = run("mai-transcribe-2", []string{"segment"}, ""); ts != "segment" || len(res.Warnings) != 0 {
		t.Errorf("segment: timestamps %v, warnings %v", ts, res.Warnings)
	}
	for _, tc := range [][2]string{{"word", "none"}, {"word", "segment"}, {"segment", "none"}} {
		res, ts := run("mai-transcribe-2", []string{tc[0]}, tc[1])
		want := []types.Warning{{Type: "unsupported", Feature: "timestampGranularities", Details: `providerOptions.azure.timestamps is "` + tc[1] + `" and overrides the requested timestamp granularities.`}}
		if ts != tc[1] || !reflect.DeepEqual(res.Warnings, want) {
			t.Errorf("%v: timestamps %v, warnings %+v", tc, ts, res.Warnings)
		}
	}
	for _, timestamps := range []string{"", "word", "segment", "none"} {
		res, ts := run("mai-transcribe-1.5", []string{"word", "segment"}, timestamps)
		want := []types.Warning{{Type: "unsupported", Feature: "timestampGranularities", Details: "MAI-Transcribe-1.5 cannot return word or segment timings."}}
		var wantTS interface{}
		if timestamps != "" {
			wantTS = timestamps
		}
		if ts != wantTS || !reflect.DeepEqual(res.Warnings, want) {
			t.Errorf("1.5 %q: timestamps %v, warnings %+v", timestamps, ts, res.Warnings)
		}
	}
}
