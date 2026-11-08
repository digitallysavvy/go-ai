package gladia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const r88provcDoneBody = `{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"x","languages":["en"],"utterances":[]}}}`

type r88provcCall struct {
	path    string
	headers http.Header
	body    map[string]interface{}
}

type r88provcGladia struct {
	mu    sync.Mutex
	calls []r88provcCall
	poll  func(n int) (int, string)
	polls int
}

func (g *r88provcGladia) snapshot() []r88provcCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]r88provcCall(nil), g.calls...)
}

func r88provcNewGladia(t *testing.T, poll func(n int) (int, string)) (*r88provcGladia, *httptest.Server) {
	t.Helper()
	g := &r88provcGladia{poll: poll}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := r88provcCall{path: r.URL.Path, headers: r.Header.Clone()}
		if r.URL.Path == "/v2/pre-recorded" {
			_ = json.Unmarshal(raw, &c.body)
		}
		g.mu.Lock()
		g.calls = append(g.calls, c)
		g.mu.Unlock()
		switch r.URL.Path {
		case "/v2/upload":
			_, _ = w.Write([]byte(`{"audio_url":"https://api.gladia.io/file/abc"}`))
		case "/v2/pre-recorded":
			_, _ = w.Write([]byte(`{"result_url":"http://` + r.Host + `/v2/job"}`))
		default:
			g.mu.Lock()
			g.polls++
			n := g.polls
			g.mu.Unlock()
			status, body := 200, r88provcDoneBody
			if g.poll != nil {
				status, body = g.poll(n)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return g, srv
}

func r88provcTranscribe(t *testing.T, srv *httptest.Server, modelID string, opts map[string]interface{}) (*types.TranscriptionResult, error) {
	t.Helper()
	m, _ := New(Config{APIKey: "test-api-key", BaseURL: srv.URL + "/v2"}).TranscriptionModel(modelID)
	var po map[string]interface{}
	if opts != nil {
		po = map[string]interface{}{"gladia": opts}
	}
	return m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg", ProviderOptions: po})
}

func r88provcInitBody(t *testing.T, g *r88provcGladia) map[string]interface{} {
	t.Helper()
	for _, c := range g.snapshot() {
		if c.path == "/v2/pre-recorded" {
			return c.body
		}
	}
	t.Fatal("no pre-recorded call")
	return nil
}

func r88provcJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TS gladia-transcription-model.test.ts (c8875b71a3): v2 options mapping.
func TestR88provcGladiaV2Options(t *testing.T) {
	t.Run("modern options reach the pre-recorded endpoint", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		_, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{
			"languageConfig":     map[string]interface{}{"languages": []string{"en"}, "codeSwitching": true},
			"piiRedaction":       true,
			"piiRedactionConfig": map[string]interface{}{"entityTypes": []string{"EMAIL_ADDRESS", "NAME"}, "processedTextType": "MARKER"},
		})
		if err != nil {
			t.Fatal(err)
		}
		got := r88provcJSON(r88provcInitBody(t, g))
		want := `{"audio_url":"https://api.gladia.io/file/abc","language_config":{"code_switching":true,"languages":["en"]},"pii_redaction":true,"pii_redaction_config":{"entity_types":["EMAIL_ADDRESS","NAME"],"processed_text_type":"MARKER"}}`
		if got != want {
			t.Fatalf("body = %s", got)
		}
	})

	t.Run("deprecated language options map to language_config", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		res, err := r88provcTranscribe(t, srv, "", map[string]interface{}{"language": "en", "enableCodeSwitching": true})
		if err != nil {
			t.Fatal(err)
		}
		if got := r88provcJSON(r88provcInitBody(t, g)["language_config"]); got != `{"code_switching":true,"languages":["en"]}` {
			t.Fatalf("language_config = %s", got)
		}
		if len(res.Warnings) != 2 || res.Warnings[0].Setting != "language" || res.Warnings[1].Setting != "enableCodeSwitching" {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
	})

	t.Run("deprecated customVocabulary array maps to custom_vocabulary_config", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		if _, err := r88provcTranscribe(t, srv, "", map[string]interface{}{"customVocabulary": []string{"Gladia", "AI SDK"}}); err != nil {
			t.Fatal(err)
		}
		body := r88provcInitBody(t, g)
		if body["custom_vocabulary"] != true || r88provcJSON(body["custom_vocabulary_config"]) != `{"vocabulary":["Gladia","AI SDK"]}` {
			t.Fatalf("body = %v", body)
		}
	})

	t.Run("legacy option warnings", func(t *testing.T) {
		for _, tc := range []struct {
			feature string
			opts    map[string]interface{}
		}{
			{"contextPrompt", map[string]interface{}{"contextPrompt": "context"}},
			{"moderation", map[string]interface{}{"moderation": true}},
			{"chapterization", map[string]interface{}{"chapterization": true}},
			{"nameConsistency", map[string]interface{}{"nameConsistency": true}},
			{"structuredDataExtraction", map[string]interface{}{"structuredDataExtraction": true}},
			{"structuredDataExtractionConfig", map[string]interface{}{"structuredDataExtractionConfig": map[string]interface{}{"classes": []string{"test"}}}},
			{"displayMode", map[string]interface{}{"displayMode": false}},
			{"diarizationConfig.enhanced", map[string]interface{}{"diarizationConfig": map[string]interface{}{"enhanced": true}}},
		} {
			g, srv := r88provcNewGladia(t, nil)
			res, err := r88provcTranscribe(t, srv, "default", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			want := []types.Warning{{Type: "unsupported", Feature: tc.feature, Details: "No longer supported by the Gladia v2 pre-recorded API; ignored."}}
			if !reflect.DeepEqual(res.Warnings, want) {
				t.Fatalf("%s: warnings = %#v", tc.feature, res.Warnings)
			}
			wantBody := `{"audio_url":"https://api.gladia.io/file/abc"}`
			if tc.feature == "diarizationConfig.enhanced" {
				wantBody = `{"audio_url":"https://api.gladia.io/file/abc","diarization_config":{}}`
			}
			if got := r88provcJSON(r88provcInitBody(t, g)); got != wantBody {
				t.Fatalf("%s: body = %s", tc.feature, got)
			}
		}
		for setting, opts := range map[string]map[string]interface{}{
			"language":            {"language": "en"},
			"detectLanguage":      {"detectLanguage": true},
			"enableCodeSwitching": {"enableCodeSwitching": false},
			"codeSwitchingConfig": {"codeSwitchingConfig": map[string]interface{}{"languages": []string{"en"}}},
		} {
			_, srv := r88provcNewGladia(t, nil)
			res, err := r88provcTranscribe(t, srv, "default", opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res.Warnings, []types.Warning{{Type: "deprecated", Setting: setting, Message: "Use languageConfig instead."}}) {
				t.Fatalf("%s: warnings = %#v", setting, res.Warnings)
			}
		}
	})

	t.Run("explicit language configuration overrides legacy fields", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		res, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{"languageConfig": map[string]interface{}{"codeSwitching": false}, "language": "en"})
		if err != nil {
			t.Fatal(err)
		}
		if got := r88provcJSON(r88provcInitBody(t, g)["language_config"]); got != `{"code_switching":false}` {
			t.Fatalf("language_config = %s", got)
		}
		want := types.Warning{Type: "deprecated", Setting: "language", Message: "Ignored because languageConfig is provided; remove this deprecated option."}
		if len(res.Warnings) != 1 || res.Warnings[0] != want {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
	})

	t.Run("legacy vocabulary validation", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		res, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{"customVocabulary": []interface{}{
			"Gladia",
			map[string]interface{}{"value": "AI SDK", "intensity": 0.5},
			nil, 42, map[string]interface{}{}, map[string]interface{}{"value": 3},
			map[string]interface{}{"value": "Invalid intensity", "intensity": "high"},
			map[string]interface{}{"value": "Invalid pronunciations", "pronunciations": []interface{}{3}},
			map[string]interface{}{"value": "Invalid language", "language": 42},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if got := r88provcJSON(r88provcInitBody(t, g)["custom_vocabulary_config"]); got != `{"vocabulary":["Gladia",{"intensity":0.5,"value":"AI SDK"}]}` {
			t.Fatalf("vocabulary = %s", got)
		}
		want := []types.Warning{
			{Type: "deprecated", Setting: "customVocabulary", Message: "Use customVocabulary: true with customVocabularyConfig instead of an array."},
			{Type: "other", Message: "Invalid customVocabulary entries were ignored; use strings or objects with a string value and correctly typed optional fields."},
		}
		if !reflect.DeepEqual(res.Warnings, want) {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
		for _, vocab := range []interface{}{[]interface{}{}, []interface{}{nil, map[string]interface{}{"value": "Invalid", "intensity": "high"}}} {
			g, srv := r88provcNewGladia(t, nil)
			if _, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{"customVocabulary": vocab}); err != nil {
				t.Fatal(err)
			}
			body := r88provcInitBody(t, g)
			if _, ok := body["custom_vocabulary"]; ok {
				t.Fatalf("custom_vocabulary sent for %v", vocab)
			}
			if _, ok := body["custom_vocabulary_config"]; ok {
				t.Fatalf("custom_vocabulary_config sent for %v", vocab)
			}
		}
		g, srv = r88provcNewGladia(t, nil)
		res, err = r88provcTranscribe(t, srv, "default", map[string]interface{}{
			"customVocabulary":       []interface{}{"Legacy"},
			"customVocabularyConfig": map[string]interface{}{"vocabulary": []interface{}{"Modern"}, "defaultIntensity": 0.4},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := r88provcInitBody(t, g)
		if r88provcJSON(body["custom_vocabulary_config"]) != `{"default_intensity":0.4,"vocabulary":["Modern"]}` || body["custom_vocabulary"] != true {
			t.Fatalf("body = %v", body)
		}
		if len(res.Warnings) != 2 || res.Warnings[1].Message != "customVocabularyConfig.vocabulary overrides the deprecated customVocabulary array." {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
	})

	t.Run("PII configuration is validated before uploading audio", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		_, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{"piiRedactionConfig": map[string]interface{}{"processedTextType": "invalid"}})
		if err == nil || !strings.Contains(err.Error(), "invalid gladia provider options") || len(g.snapshot()) != 0 {
			t.Fatalf("err = %v, calls = %d", err, len(g.snapshot()))
		}
		for _, bad := range []map[string]interface{}{{"languageConfig": nil}, {"piiRedaction": nil}, {"translationConfig": map[string]interface{}{"targetLanguages": []interface{}{}, "context": nil}}} {
			if _, err := r88provcTranscribe(t, srv, "default", bad); err == nil {
				t.Fatalf("%v accepted", bad)
			}
		}
	})

	t.Run("PII configuration without enabling redaction warns", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		res, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{"piiRedactionConfig": map[string]interface{}{"processedTextType": "MASK"}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res.Warnings, []types.Warning{{Type: "other", Message: "piiRedactionConfig has no effect unless piiRedaction is true."}}) {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
		if got := r88provcJSON(r88provcInitBody(t, g)["pii_redaction_config"]); got != `{"processed_text_type":"MASK"}` {
			t.Fatalf("pii_redaction_config = %s", got)
		}
	})

	t.Run("current translation and audio-to-LLM options", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		_, err := r88provcTranscribe(t, srv, "default", map[string]interface{}{
			"translation": true,
			"translationConfig": map[string]interface{}{
				"targetLanguages": []string{"fr"}, "model": "batch", "lipsync": false,
				"contextAdaptation": true, "context": "Science", "informal": false,
			},
			"audioToLlm":       true,
			"audioToLlmConfig": map[string]interface{}{"prompts": []string{"Summarize"}, "model": "openai/gpt-5.4-nano"},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := r88provcInitBody(t, g)
		if got := r88provcJSON(body["translation_config"]); got != `{"context":"Science","context_adaptation":true,"informal":false,"lipsync":false,"model":"batch","target_languages":["fr"]}` {
			t.Fatalf("translation_config = %s", got)
		}
		if got := r88provcJSON(body["audio_to_llm_config"]); got != `{"model":"openai/gpt-5.4-nano","prompts":["Summarize"]}` {
			t.Fatalf("audio_to_llm_config = %s", got)
		}
	})

	t.Run("typed options are read like a map", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		ctx := "Science"
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL + "/v2"}).TranscriptionModel("")
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg",
			ProviderOptions: map[string]interface{}{"gladia": TranscriptionModelOptions{
				LanguageConfig:    &LanguageConfig{Languages: []string{"de"}},
				TranslationConfig: &TranslationConfig{TargetLanguages: []string{"fr"}, Context: &ctx},
			}}})
		if err != nil {
			t.Fatal(err)
		}
		body := r88provcInitBody(t, g)
		if r88provcJSON(body["language_config"]) != `{"languages":["de"]}` || r88provcJSON(body["translation_config"]) != `{"context":"Science","target_languages":["fr"]}` {
			t.Fatalf("body = %v", body)
		}
	})
}

// TS c8875b71a3 model selection, under the coordinator ruling (round 88,
// S6): "" / "default" omitted, solaria-* sent, any other ID not sent with an
// unsupported warning. Response.ModelID reports the ID.
func TestR88provcGladiaModelID(t *testing.T) {
	for _, tc := range []struct {
		id, wantModel, wantResponse string
		warn                        bool
	}{
		{"", "", "default", false},
		{"default", "", "default", false},
		{"solaria-1", "solaria-1", "solaria-1", false},
		{ModelSolaria3, "solaria-3", "solaria-3", false},
		{"whisper-v3", "", "whisper-v3", true},
	} {
		g, srv := r88provcNewGladia(t, nil)
		opts := map[string]interface{}{"languageConfig": map[string]interface{}{"languages": []string{"fr"}}}
		res, err := r88provcTranscribe(t, srv, tc.id, opts)
		if err != nil {
			t.Fatal(err)
		}
		body := r88provcInitBody(t, g)
		model, has := body["model"]
		if (tc.wantModel == "" && has) || (tc.wantModel != "" && model != tc.wantModel) {
			t.Fatalf("%q: model = %v", tc.id, body["model"])
		}
		if res.Response.ModelID != tc.wantResponse {
			t.Fatalf("%q: response model = %q", tc.id, res.Response.ModelID)
		}
		if tc.warn != (len(res.Warnings) == 1 && res.Warnings[0].Type == "unsupported" && res.Warnings[0].Feature == "modelId") || (!tc.warn && len(res.Warnings) != 0) {
			t.Fatalf("%q: warnings = %#v", tc.id, res.Warnings)
		}
		for _, c := range g.snapshot() {
			if c.headers.Get("x-gladia-key") != "test-api-key" || c.headers.Get("x-gladia-version") != version.SDKUserAgent() {
				t.Fatalf("%q %s headers = %v", tc.id, c.path, c.headers)
			}
		}
	}

	t.Run("solaria-3 warnings", func(t *testing.T) {
		_, srv := r88provcNewGladia(t, nil)
		res, err := r88provcTranscribe(t, srv, "solaria-3", map[string]interface{}{"languageConfig": map[string]interface{}{"languages": []string{"fr", "en"}, "codeSwitching": true}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Warnings) != 2 || !strings.Contains(res.Warnings[0].Message, "single language") || !strings.Contains(res.Warnings[1].Message, "code switching") {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
		g, srv := r88provcNewGladia(t, nil)
		res, err = r88provcTranscribe(t, srv, "solaria-3", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := r88provcJSON(r88provcInitBody(t, g)); got != `{"audio_url":"https://api.gladia.io/file/abc","model":"solaria-3"}` {
			t.Fatalf("body = %s", got)
		}
		want := []types.Warning{{Type: "other", Message: `The "solaria-3" model supports a single language only. Pass exactly one language in languageConfig.languages; Gladia may reject the request.`}}
		if !reflect.DeepEqual(res.Warnings, want) {
			t.Fatalf("warnings = %#v", res.Warnings)
		}
	})

	t.Run("workflow round trip keeps the model ID", func(t *testing.T) {
		for _, id := range []string{"default", "solaria-3"} {
			m, _ := New(Config{APIKey: "k"}).TranscriptionModel(id)
			restored, err := provider.DeserializeTranscriptionModel(m.(*TranscriptionModel).Serialize())
			if err != nil || restored.ModelID() != id {
				t.Fatalf("%s: %v %v", id, restored, err)
			}
		}
	})

	t.Run("a caller x-gladia-version header wins", func(t *testing.T) {
		g, srv := r88provcNewGladia(t, nil)
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL + "/v2", Headers: map[string]string{"X-Gladia-Version": "custom"}}).TranscriptionModel("")
		if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err != nil {
			t.Fatal(err)
		}
		for _, c := range g.snapshot() {
			if v := c.headers.Values("X-Gladia-Version"); len(v) != 1 || v[0] != "custom" {
				t.Fatalf("%s: x-gladia-version = %v", c.path, v)
			}
		}
	})
}

// TS gladia-transcription-options-resolution.test.ts.
func TestR88provcGladiaResolveLanguageConfig(t *testing.T) {
	type out struct {
		languages     []interface{}
		codeSwitching interface{}
		ok            bool
	}
	cases := []struct {
		in   map[string]interface{}
		want out
	}{
		{map[string]interface{}{}, out{}},
		{map[string]interface{}{"languageConfig": map[string]interface{}{"languages": []interface{}{"en"}, "codeSwitching": true}}, out{[]interface{}{"en"}, true, true}},
		{map[string]interface{}{"language": "en"}, out{[]interface{}{"en"}, nil, true}},
		{map[string]interface{}{"enableCodeSwitching": true, "codeSwitchingConfig": map[string]interface{}{"languages": []interface{}{"en", "fr"}}}, out{[]interface{}{"en", "fr"}, true, true}},
		{map[string]interface{}{"detectLanguage": true}, out{[]interface{}{}, nil, true}},
		{map[string]interface{}{"language": "en", "languageConfig": map[string]interface{}{"languages": []interface{}{"de"}}}, out{[]interface{}{"de"}, nil, true}},
		{map[string]interface{}{"languageConfig": map[string]interface{}{"codeSwitching": false}, "language": "en", "enableCodeSwitching": true, "codeSwitchingConfig": map[string]interface{}{"languages": []interface{}{"fr"}}}, out{nil, false, true}},
		{map[string]interface{}{"languageConfig": map[string]interface{}{}, "language": "en"}, out{nil, nil, true}},
		{map[string]interface{}{"codeSwitchingConfig": map[string]interface{}{}}, out{}},
		{map[string]interface{}{"language": "en", "enableCodeSwitching": true, "codeSwitchingConfig": map[string]interface{}{"languages": []interface{}{"en", "fr"}}}, out{[]interface{}{"en", "fr"}, true, true}},
		{map[string]interface{}{"enableCodeSwitching": false, "codeSwitchingConfig": map[string]interface{}{"languages": []interface{}{"fr"}}}, out{nil, false, true}},
		{map[string]interface{}{"detectLanguage": false}, out{}},
	}
	for i, tc := range cases {
		languages, codeSwitching, ok := resolveGladiaLanguageConfig(tc.in)
		if !reflect.DeepEqual(out{languages, codeSwitching, ok}, tc.want) {
			t.Fatalf("case %d: got %v %v %v, want %+v", i, languages, codeSwitching, ok, tc.want)
		}
	}

	var warnings []types.Warning
	cv, vocab, have := resolveGladiaCustomVocabulary(map[string]interface{}{"customVocabulary": []interface{}{"Gladia", "AI SDK"}}, &warnings)
	if cv == nil || !*cv || !have || !reflect.DeepEqual(vocab, []interface{}{"Gladia", "AI SDK"}) || len(warnings) != 1 {
		t.Fatalf("array: %v %v %v %v", cv, vocab, have, warnings)
	}
	warnings = nil
	cv, vocab, have = resolveGladiaCustomVocabulary(map[string]interface{}{"customVocabulary": true}, &warnings)
	if cv == nil || !*cv || have || vocab != nil || len(warnings) != 0 {
		t.Fatalf("bool: %v %v %v %v", cv, vocab, have, warnings)
	}
}
