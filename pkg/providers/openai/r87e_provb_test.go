package openai

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r87eprovbFormFields parses a multipart transcription body into its
// non-file fields, in order.
func r87eprovbFormFields(t *testing.T, body io.Reader, contentType string) [][2]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("content type %q: %v", contentType, err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	var fields [][2]string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return fields
		}
		if err != nil {
			t.Fatalf("multipart: %v", err)
		}
		if part.FileName() != "" {
			continue
		}
		value, _ := io.ReadAll(part)
		fields = append(fields, [2]string{part.FormName(), string(value)})
	}
}

func r87eprovbHasField(fields [][2]string, name string) (string, bool) {
	for _, f := range fields {
		if f[0] == name {
			return f[1], true
		}
	}
	return "", false
}

// TS openai-transcription-model.ts getArgs appends every option whose value
// is `!= null`, so a present `language: ""` / `prompt: ""` is sent as an
// empty form field; an absent one is not sent.
func TestR87eprovbTranscriptionEmptyLanguageAndPromptAreSent(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "gpt-4o-transcribe")
	body, ct, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
		Language: "fr", // Go-only top-level language: the present provider option wins
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"language": "",
			"prompt":   "",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := r87eprovbFormFields(t, body, ct)
	if v, ok := r87eprovbHasField(fields, "language"); !ok || v != "" {
		t.Fatalf("language = %q (present %v), want present \"\"; fields %v", v, ok, fields)
	}
	if v, ok := r87eprovbHasField(fields, "prompt"); !ok || v != "" {
		t.Fatalf("prompt = %q (present %v), want present \"\"; fields %v", v, ok, fields)
	}

	// Absent keys: no prompt; the top-level Go language still applies.
	body, ct, err = m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:           []byte("audio"),
		MimeType:        "audio/mpeg",
		Language:        "fr",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields = r87eprovbFormFields(t, body, ct)
	if _, ok := r87eprovbHasField(fields, "prompt"); ok {
		t.Fatalf("absent prompt was sent: %v", fields)
	}
	if v, ok := r87eprovbHasField(fields, "language"); !ok || v != "fr" {
		t.Fatalf("language = %q (present %v), want fr", v, ok)
	}

	// No options at all and no top-level language: no language field.
	body, ct, err = m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("audio"), MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r87eprovbHasField(r87eprovbFormFields(t, body, ct), "language"); ok {
		t.Fatal("language sent without any language option")
	}
}

// TS doStream: `providerOptions?.language != null ? {language} : {}` (or
// `languages: [language]` for the languageField models), so "" is sent.
func TestR87eprovbStreamingTranscriptionEmptyLanguageIsSent(t *testing.T) {
	format := provider.AudioFormat{Type: "audio/pcm"}
	for _, tc := range []struct {
		model string
		key   string
	}{
		{"gpt-realtime-whisper", "language"},
		{"gpt-live-transcribe", "languages"},
	} {
		opts := TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]interface{}{"language": ""}})
		session := BuildOpenAIRealtimeTranscriptionSession(tc.model, format, opts)
		transcription := session["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})
		v, ok := transcription[tc.key]
		if !ok {
			t.Fatalf("%s: %s not sent: %v", tc.model, tc.key, transcription)
		}
		if tc.key == "languages" {
			if list, _ := v.([]string); len(list) != 1 || list[0] != "" {
				t.Fatalf("%s: languages = %v, want [\"\"]", tc.model, v)
			}
		} else if v != "" {
			t.Fatalf("%s: language = %v, want \"\"", tc.model, v)
		}

		absent := BuildOpenAIRealtimeTranscriptionSession(tc.model, format, TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]interface{}{}}))
		at := absent["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})
		if _, ok := at[tc.key]; ok {
			t.Fatalf("%s: absent language sent: %v", tc.model, at)
		}
	}
	// A v0.5-style literal without the presence flag still sends a language.
	lit := BuildOpenAIRealtimeTranscriptionSession("gpt-realtime-whisper", format, TranscriptionStreamOpenAIOptionsValue{Language: "de"})
	if got := lit["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})["language"]; got != "de" {
		t.Fatalf("literal language = %v, want de", got)
	}
}

// TS mapComputerCallInput: `...(safetyCheck.code != null && {code})`, so a
// present "" code / message stays in the tool input and a null or absent one
// is dropped. Generate and stream.
func TestR87eprovbComputerSafetyCheckEmptyStringsKept(t *testing.T) {
	const item = `{"type":"computer_call","id":"cu_1","call_id":"call_1","status":"completed","actions":[],` +
		`"pending_safety_checks":[{"id":"a","code":"","message":""},{"id":"b","code":null},{"id":"c","code":"x","message":"y"}]}`
	const want = `{"actions":[],"pendingSafetyChecks":[{"id":"a","code":"","message":""},{"id":"b"},{"id":"c","code":"x","message":"y"}],"status":"completed"}`

	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		Output: []json.RawMessage{json.RawMessage(item)},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	if got := result.ToolCalls[0].RawArguments; got != want {
		t.Fatalf("generate input =\n%s\nwant\n%s", got, want)
	}

	stream := newResponsesStream(io.NopCloser(strings.NewReader(
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"computer_call","id":"cu_1","status":"in_progress"}}`+"\n\n"+
			`data: {"type":"response.output_item.done","output_index":0,"item":`+item+`}`+"\n\n")), false)
	defer stream.Close() //nolint:errcheck
	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatalf("stream ended without a tool call: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			if got := chunk.ToolCall.RawArguments; got != want {
				t.Fatalf("stream input =\n%s\nwant\n%s", got, want)
			}
			return
		}
	}
}
