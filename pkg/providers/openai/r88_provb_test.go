package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r88provbChatStream serves frames as an SSE body to a chat model and returns
// every stream part.
func r88provbChatStream(t *testing.T, frames []string, compat bool) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	cfg := Config{APIKey: "k", BaseURL: srv.URL}
	if compat {
		cfg.OpenAICompatible, cfg.Name = true, "test-provider"
	}
	m, err := New(cfg).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var out []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

// TS f6dbf04b5d (openai-chat-language-model.ts doStream): an empty
// `content: ""` delta is emitted only when the chunk has no other text delta;
// a refusal in the same chunk replaces it.
func TestR88provbChatStreamEmptyContentWithRefusal(t *testing.T) {
	parts := r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"","refusal":"No."},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	var deltas []string
	var finish *provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeText:
			deltas = append(deltas, p.Text)
		case provider.ChunkTypeFinish:
			finish = p
		}
	}
	if len(deltas) != 1 || deltas[0] != "No." {
		t.Fatalf("text deltas = %q, want [\"No.\"]", deltas)
	}
	if finish == nil || finish.FinishReason != "content-filter" || finish.RawFinishReason != "stop" {
		t.Fatalf("finish = %+v, want content-filter / stop", finish)
	}

	// Without a refusal the empty content delta keeps its v0.5 behavior.
	parts = r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	deltas = nil
	for _, p := range parts {
		if p.Type == provider.ChunkTypeText {
			deltas = append(deltas, p.Text)
		}
	}
	if len(deltas) != 1 || deltas[0] != "" {
		t.Fatalf("text deltas = %q, want [\"\"]", deltas)
	}
}

// TS 6b97088662 (openai-responses-language-model.test.ts "refusal" block):
// refusal content parts become text parts with {itemId, phase} metadata, the
// structured output text includes them, and the finish reason is
// content-filter unless a client tool call is present. These cases bypass
// the response schema (provg CR: the refusal member of the message content
// union); the stream cases replay through TestTSCasesResponses.
func TestR88provbResponsesRefusalParts(t *testing.T) {
	m := &ResponsesLanguageModel{}
	for _, tc := range []struct {
		name       string
		body       string
		texts      []string
		finish     types.FinishReason
		structured *string
		types      []string
	}{
		{
			name:   "output_text and refusal",
			body:   `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_mixed","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Here is what I can share.","annotations":[]},{"type":"refusal","refusal":"I can't share the rest."}]}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:  []string{"Here is what I can share.", "I can't share the rest."},
			finish: types.FinishReasonContentFilter,
		},
		{
			name:       "final answer refusal",
			body:       `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_final","type":"message","status":"completed","role":"assistant","phase":"final_answer","content":[{"type":"refusal","refusal":"I'm sorry, but I can't assist with that request."}]}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:      []string{"I'm sorry, but I can't assist with that request."},
			finish:     types.FinishReasonContentFilter,
			structured: r88provbStrPtr("I'm sorry, but I can't assist with that request."),
		},
		{
			name:   "refusal with a function call",
			body:   `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_function_call","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"I can't do that, but I can check the weather."}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"location\":\"Paris\"}","status":"completed"}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:  []string{"I can't do that, but I can check the weather."},
			finish: types.FinishReasonToolCalls,
			types:  []string{"text", "tool-call"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp responses.ResponsesAPIResponse
			if err := json.Unmarshal([]byte(tc.body), &resp); err != nil {
				t.Fatal(err)
			}
			res, err := m.convertResponse(resp, true, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var texts, kinds []string
			for _, p := range res.Content {
				switch v := p.(type) {
				case types.TextContent:
					texts = append(texts, v.Text)
					kinds = append(kinds, "text")
					meta, _ := v.ProviderOptions["openai"].(map[string]interface{})
					if meta["itemId"] == nil || meta["annotations"] != nil {
						t.Errorf("text metadata = %v, want itemId only", v.ProviderOptions)
					}
				default:
					kinds = append(kinds, "tool-call")
				}
			}
			if strings.Join(texts, "|") != strings.Join(tc.texts, "|") {
				t.Errorf("texts = %q, want %q", texts, tc.texts)
			}
			if tc.types != nil && strings.Join(kinds, ",") != strings.Join(tc.types, ",") {
				t.Errorf("part types = %v, want %v", kinds, tc.types)
			}
			if res.FinishReason != tc.finish || res.RawFinishReason != "" {
				t.Errorf("finish = %s / %q, want %s / undefined", res.FinishReason, res.RawFinishReason, tc.finish)
			}
			if tc.structured != nil {
				got := responsesStructuredOutputText(resp.Output)
				if got == nil || *got != *tc.structured {
					t.Errorf("structuredOutputText = %v, want %q", got, *tc.structured)
				}
			}
		})
	}
}

func r88provbStrPtr(s string) *string { return &s }

// TS 6b97088662: response.refusal.delta and response.refusal.done each mark
// the response as refused (content-filter with no incomplete reason).
func TestR88provbResponsesStreamRefusalFlag(t *testing.T) {
	head := []string{
		`{"type":"response.created","response":{"id":"r","created_at":1,"model":"gpt-4o","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
	}
	tail := []string{
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.completed","response":{"id":"r","created_at":1,"model":"gpt-4o","status":"completed","incomplete_details":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
	}
	for name, mid := range map[string][]string{
		"delta only": {`{"type":"response.refusal.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"No."}`},
		"done only":  {`{"type":"response.refusal.done","item_id":"msg_1","output_index":0,"content_index":0,"refusal":"No."}`},
	} {
		t.Run(name, func(t *testing.T) {
			frames := append(append(append([]string{}, head...), mid...), tail...)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				for _, f := range frames {
					_, _ = w.Write([]byte("data: " + f + "\n\n"))
				}
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
			if err != nil {
				t.Fatal(err)
			}
			st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			var finish *provider.StreamChunk
			var text string
			for {
				c, err := st.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeText {
					text += c.Text
				}
				if c.Type == provider.ChunkTypeFinish {
					finish = c
				}
			}
			if finish == nil || finish.FinishReason != types.FinishReasonContentFilter {
				t.Fatalf("finish = %+v, want content-filter", finish)
			}
			if name == "delta only" && text != "No." {
				t.Fatalf("text = %q, want the refusal delta", text)
			}
		})
	}
}

// TS 6b97088662 (openai-batch.test.ts "preserves refusal content parts and
// maps the finish reason to content-filter").
func TestR88provbBatchRefusal(t *testing.T) {
	res, berr := convertOpenAIBatchResponseBodyBytes(json.RawMessage(`{"id":"resp_1","created_at":1,"model":"gpt-4o","output":[{"type":"message","role":"assistant","id":"msg_123","phase":null,"content":[{"type":"refusal","refusal":"I'm sorry, I can't assist with that."}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	if berr != nil {
		t.Fatal(berr)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %#v, want one text part", res.Content)
	}
	if tc, ok := res.Content[0].(types.TextContent); !ok || tc.Text != "I'm sorry, I can't assist with that." {
		t.Fatalf("content = %#v", res.Content)
	}
	if res.FinishReason != types.FinishReasonContentFilter || res.RawFinishReason != "" {
		t.Fatalf("finish = %s / %q", res.FinishReason, res.RawFinishReason)
	}
}

// r88provbChatMessages builds the OpenAI chat request messages and warnings
// for msgs (TS convertToOpenAIChatMessages through getArgs).
func r88provbChatMessages(t *testing.T, msgs []types.Message, providerOptions map[string]interface{}) (string, []types.Warning, error) {
	t.Helper()
	m, err := New(Config{APIKey: "k"}).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	body, warnings, err := m.(*LanguageModel).buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}, ProviderOptions: providerOptions}, false)
	if err != nil {
		return "", warnings, err
	}
	raw, _ := json.Marshal(body["messages"])
	return string(raw), warnings, nil
}

// TS 243bf221a8 (convert-to-openai-chat-messages.test.ts "message names"):
// the it.each cases the extractor cannot expand.
func TestR88provbChatMessageNames(t *testing.T) {
	bp := map[string]interface{}{"mode": "explicit"}
	for _, mode := range []string{"system", "developer"} {
		msgs := []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Be helpful"}},
				ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"name": "instructions", "promptCacheBreakpoint": bp}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheBreakpoint": bp}}}},
				ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"name": "bot"}}},
		}
		got, _, err := r88provbChatMessages(t, msgs, map[string]interface{}{"openai": map[string]interface{}{"systemMessageMode": mode}})
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"content":[{"prompt_cache_breakpoint":{"mode":"explicit"},"text":"Be helpful","type":"text"}],"name":"instructions","role":"` + mode + `"},{"content":[{"prompt_cache_breakpoint":{"mode":"explicit"},"text":"Hi","type":"text"}],"name":"bot","role":"assistant"}]`
		if got != want {
			t.Errorf("%s mode:\n got %s\nwant %s", mode, got, want)
		}
	}

	for _, name := range []interface{}{nil, 42, true, map[string]interface{}{}, []interface{}{}} {
		msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"name": name}}}}
		_, _, err := r88provbChatMessages(t, msgs, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid openai provider options") {
			t.Errorf("name %#v: err = %v, want invalid openai provider options", name, err)
		}
	}

	// The Go-only Message.Name keeps working; the TS option wins.
	got, _, err := r88provbChatMessages(t, []types.Message{
		{Role: types.RoleUser, Name: "legacy", Content: []types.ContentPart{types.TextContent{Text: "a"}}},
		{Role: types.RoleUser, Name: "legacy", Content: []types.ContentPart{types.TextContent{Text: "b"}},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"name": "alice"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"content":"a","name":"legacy","role":"user"},{"content":"b","name":"alice","role":"user"}]`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// The OpenAI-compatible converter does not read the option.
	srvBody := map[string]interface{}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &srvBody)
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	cm, _ := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider"}).ChatModel("m")
	if _, err := cm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"name": 42}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(srvBody["messages"]); string(raw) != `[{"content":"a","role":"user"}]` {
		t.Errorf("compat messages = %s", raw)
	}
}

// r88provbImageServer records the image requests: the JSON body, or the
// multipart field order, values and part content types.
type r88provbImageRequest struct {
	json   map[string]interface{}
	fields []string
	values map[string]string
	types  map[string]string
}

func r88provbImageServer(t *testing.T) (*httptest.Server, *[]r88provbImageRequest) {
	t.Helper()
	var reqs []r88provbImageRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := r88provbImageRequest{values: map[string]string{}, types: map[string]string{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			mr, err := r.MultipartReader()
			if err == nil {
				for {
					part, err := mr.NextPart()
					if err != nil {
						break
					}
					v, _ := io.ReadAll(part)
					rec.fields = append(rec.fields, part.FormName())
					rec.values[part.FormName()] = string(v)
					rec.types[part.FormName()] = part.Header.Get("Content-Type")
				}
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&rec.json)
		}
		reqs = append(reqs, rec)
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGk="}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// TS 6549344e0a / 5f9bb8eea4 (openai-image-model.test.ts "GPT Image 2.6
// options" and the edit moderation case).
func TestR88provbImageGPTImage26Options(t *testing.T) {
	srv, reqs := r88provbImageServer(t)
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelGPTImage26Flare)
	n := 1
	png := provider.ImageFile{Type: "file", MediaType: "image/png", Data: []byte{137, 80, 78, 71}}
	ctx := context.Background()

	if _, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", N: &n, Size: "1024x1024", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
		"complexity": "low", "outputFormat": "gif", "frameCount": 12, "frameDurationMs": 100, "safetyIdentifier": "hashed-user",
	}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal((*reqs)[0].json)
	if want := `{"complexity":"low","frame_count":12,"frame_duration_ms":100,"model":"gpt-image-2.6-flare","n":1,"output_format":"gif","prompt":"p","safety_identifier":"hashed-user","size":"1024x1024"}`; string(got) != want {
		t.Errorf("generation body = %s, want %s", got, want)
	}

	// gif is generation only.
	if _, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", Files: []provider.ImageFile{png}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"outputFormat": "gif"}}}); err == nil {
		t.Error("gif edit: want an error")
	}
	// safetyIdentifier has at most 128 characters.
	if _, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": strings.Repeat("a", 129)}}}); err == nil {
		t.Error("safetyIdentifier 129: want an error")
	}

	*reqs = nil
	if _, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", N: &n, Size: "1024x1024", Files: []provider.ImageFile{png}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
		"complexity": "medium", "safetyIdentifier": "hashed-user", "moderation": "low", "quality": "high", "inputFidelity": "high",
		"markup": "data:image/png;base64,iVBORw==",
	}}}); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if got, want := strings.Join(r.fields, ","), "model,prompt,image,markup,n,size,quality,complexity,input_fidelity,moderation,safety_identifier"; got != want {
		t.Errorf("edit field order = %s, want %s", got, want)
	}
	if r.types["markup"] != "image/png" || r.values["markup"] != string([]byte{137, 80, 78, 71}) {
		t.Errorf("markup part = %q (%s)", r.values["markup"], r.types["markup"])
	}
	if r.values["moderation"] != "low" || r.values["complexity"] != "medium" || r.values["safety_identifier"] != "hashed-user" {
		t.Errorf("edit values = %v", r.values)
	}

	*reqs = nil
	for markup, want := range map[string]string{
		"data:image/png;base64,iVBORw0KGgo=": "`markup` cannot be combined with `mask`.",
	} {
		_, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", Files: []provider.ImageFile{png}, Mask: &png, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"markup": markup}}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("markup + mask: err = %v, want %q", err, want)
		}
	}
	_, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p", Files: []provider.ImageFile{png}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"markup": "data:image/png,abc"}}})
	if err == nil || !strings.Contains(err.Error(), "`markup` data URLs must be base64 encoded.") {
		t.Errorf("plain data URL: err = %v", err)
	}
	if len(*reqs) != 0 {
		t.Errorf("%d requests sent, want none", len(*reqs))
	}

	if f := m.SupportsFileInputs(); f == nil || !*f {
		t.Error("SupportsFileInputs: want true for gpt-image-2.6-flare")
	}
	if ImageMaxImagesPerCall(ModelGPTImage26Sunburst) != 10 {
		t.Error("max images: want 10")
	}
}
