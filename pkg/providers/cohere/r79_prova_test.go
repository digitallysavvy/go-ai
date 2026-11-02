package cohere

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// TestR79prova_TSCasesChatAsserts replays cohere-chat-language-model.test.ts
// extracted at e64ed82f27 with --asserts (plain assertions as well as
// snapshots, path snapshots included).
func TestR79prova_TSCasesChatAsserts(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_asserts.json.gz")
	var plain, headers []tsfixture.Case
	for _, c := range cases {
		c.Call, _ = r79provaBuffers(c.Call).(map[string]interface{})
		// `const { usage } = await model.doGenerate(...)`: an alias the
		// extractor does not record
		if c.Test == "should preserve extra top-level and nested fields in raw usage" && c.CallKind == "generate" {
			c.Aliases = map[string]string{"usage": "result.usage"}
		}
		if c.Test == "should pass headers" {
			headers = append(headers, c)
			continue
		}
		plain = append(plain, c)
	}
	tsfixture.EvalPathSnapshots(t)
	run := func(cases []tsfixture.Case, h map[string]string) {
		tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
			if id == "" {
				id = "command-r-plus"
			}
			m, err := New(Config{APIKey: "test-api-key", BaseURL: base, Headers: h}).LanguageModel(id)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}, nil)
	}
	run(plain, nil)
	// createCohere({apiKey: 'test-api-key', headers: {...}}) in the TS test
	run(headers, map[string]string{"Custom-Provider-Header": "provider-header-value"})
}

// r79provaBuffers rewrites the extractor's form of a TS Buffer.from(...)
// value ({type: 'Buffer', data: [bytes]}) into the __bytes marker the
// harness turns into file bytes.
func r79provaBuffers(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if x["type"] == "Buffer" && len(x) == 2 {
			if arr, ok := x["data"].([]interface{}); ok {
				b := make([]byte, 0, len(arr))
				for _, e := range arr {
					f, _ := e.(float64)
					b = append(b, byte(f))
				}
				return map[string]interface{}{"__bytes": base64.StdEncoding.EncodeToString(b)}
			}
		}
		for k, e := range x {
			x[k] = r79provaBuffers(e)
		}
	case []interface{}:
		for i, e := range x {
			x[i] = r79provaBuffers(e)
		}
	}
	return v
}

// TestR79prova_TSCasesConvertMessages replays
// convert-to-cohere-chat-prompt.test.ts ({messages, documents, warnings}).
func TestR79prova_TSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	for i, c := range cases {
		// `const userMessage = result.messages[0]` and the cast `part`: aliases
		// the extractor does not record
		switch c.Test {
		case "should omit detail when no provider option is set", `should accept top-level "image" media type and detect full type from bytes`:
			cases[i].Aliases = map[string]string{"result": "result", "userMessage": "result.messages[0]", "part": "result.messages[0].content[0]"}
		}
	}
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		msgs, why := tsfixture.BuildPromptMessages(r79provaBuffers(args[0]))
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		if err := prompt.RejectProviderReferenceFiles(msgs); err != nil {
			return nil, nil, err
		}
		m := &LanguageModel{modelID: "command-r-plus"}
		body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}})
		if err != nil {
			return nil, nil, err
		}
		documents := body["documents"]
		if documents == nil {
			documents = []interface{}{}
		}
		return map[string]interface{}{
			"messages":  body["messages"],
			"documents": documents,
			"warnings":  tsfixture.WarningsToTS(warnings),
		}, nil, nil
	}, nil)
}

// TestR79prova_TSCasesPrepareTools replays cohere-prepare-tools.test.ts.
func TestR79prova_TSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		tc, hasChoice := in["toolChoice"].(map[string]interface{})
		if hasChoice {
			choice = tsfixture.BuildToolChoice(tc)
		}
		tools, toolChoice, warnings := prepareCohereTools(ts, choice, hasChoice)
		out := map[string]interface{}{"toolWarnings": tsfixture.WarningsToTS(warnings)}
		if tools != nil {
			out["tools"] = tools
		}
		if toolChoice != nil {
			out["toolChoice"] = toolChoice
		}
		return out, nil, nil
	}, nil)
}

// TestR79prova_TopLevelImageMediaTypeDetected covers the regex assertion the
// extractor does not record in convert-to-cohere-chat-prompt.test.ts "should
// accept top-level "image" media type and detect full type from bytes"
// (expect(url).toMatch(/^data:image\/png;base64,/)).
func TestR79prova_TopLevelImageMediaTypeDetected(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	m := &LanguageModel{modelID: "command-r-plus"}
	body, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: png, MediaType: "image"}, MediaType: "image"}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	msgs := body["messages"].([]map[string]interface{})
	parts := msgs[0]["content"].([]map[string]interface{})
	url := parts[0]["image_url"].(map[string]interface{})["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url = %q", url)
	}
}

func r79provacohereDrain(t *testing.T, body string) []*provider.StreamChunk {
	t.Helper()
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
	defer func() { _ = s.Close() }()
	var out []*provider.StreamChunk
	for i := 0; i < 1000; i++ {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("stream error: %v", err)
			}
			return out
		}
		out = append(out, c)
	}
	t.Fatal("stream did not end")
	return nil
}

// TS cohereChatChunkSchema: index and the usage token counts are z.number(),
// so a fractional value parses; ids are String(value.index). Go decoded them
// into int and turned the whole chunk into an error part.
func TestR79prova_StreamFractionalNumbers(t *testing.T) {
	body := `data: {"type":"message-start","id":"m1"}` + "\n\n" +
		`data: {"type":"content-start","index":0.5,"delta":{"message":{"content":{"type":"text","text":""}}}}` + "\n\n" +
		`data: {"type":"content-delta","index":0.5,"delta":{"message":{"content":{"text":"Hi"}}}}` + "\n\n" +
		`data: {"type":"content-end","index":0.5}` + "\n\n" +
		`data: {"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":3.5,"output_tokens":2},"cached_tokens":1.5}}}` + "\n\n"
	parts := r79provacohereDrain(t, body)
	var ids []string
	var finish *provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeError:
			t.Fatalf("unexpected error part: %s", p.Text)
		case provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd:
			ids = append(ids, p.ID)
		case provider.ChunkTypeFinish:
			finish = p
		}
	}
	if strings.Join(ids, ",") != "0.5,0.5,0.5" {
		t.Fatalf("ids = %v", ids)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 3 {
		t.Fatalf("finish = %+v", finish)
	}
	if raw, _ := finish.Usage.Raw["cached_tokens"].(float64); raw != 1.5 {
		t.Fatalf("raw usage = %v", finish.Usage.Raw)
	}
}

// TS: every message-start emits response-metadata ({id: value.id ??
// undefined}), also without an id.
func TestR79prova_StreamMessageStartWithoutID(t *testing.T) {
	for _, start := range []string{`{"type":"message-start"}`, `{"type":"message-start","id":null}`} {
		parts := r79provacohereDrain(t, "data: "+start+"\n\n")
		if len(parts) != 2 || parts[0].Type != provider.ChunkTypeResponseMetadata || parts[0].ResponseMetadata == nil || parts[0].ResponseMetadata.ID != "" || parts[1].Type != provider.ChunkTypeFinish {
			t.Fatalf("%s: parts = %+v", start, parts)
		}
	}
}

// TS cohereChatResponseSchema: citation start/end and the usage counts are
// z.number(); a fractional value parses (Go failed the whole response).
func TestR79prova_GenerateFractionalNumbers(t *testing.T) {
	body := `{"generation_id":"g","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}],"citations":[{"start":0.5,"end":2,"text":"Hi","sources":[{"type":"document","document":{"text":"d","title":"T"}}]}]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":4.5,"output_tokens":2}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("command-r-plus")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 4 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	var src *types.SourceContent
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			src = &s
		}
	}
	if src == nil || !strings.Contains(string(src.ProviderMetadata), `"start":0.5`) {
		t.Fatalf("source = %+v", src)
	}
}

// TS content-delta content is z.union([{text}, {thinking}]): the first member
// that parses wins and strips the other key, so a delta with a string text is
// a text-delta even when it also has a thinking key.
func TestR79prova_StreamDeltaTextWinsOverThinking(t *testing.T) {
	for _, content := range []string{`{"text":"a","thinking":"b"}`, `{"text":"a","thinking":5}`} {
		body := `data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text","text":""}}}}` + "\n\n" +
			`data: {"type":"content-delta","index":0,"delta":{"message":{"content":` + content + `}}}` + "\n\n"
		var got []string
		for _, p := range r79provacohereDrain(t, body) {
			switch p.Type {
			case provider.ChunkTypeText:
				got = append(got, "text:"+p.Text)
			case provider.ChunkTypeReasoning:
				got = append(got, "reasoning:"+p.Reasoning)
			case provider.ChunkTypeError:
				got = append(got, "error")
			}
		}
		if strings.Join(got, ",") != "text:a" {
			t.Fatalf("%s: got %v", content, got)
		}
	}
}

// TS doGenerate pushes each non-empty text and thinking item as its own part
// in wire order; Go merged the text into one lead part after all reasoning.
func TestR79prova_GenerateContentOrder(t *testing.T) {
	body := `{"message":{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"thinking","thinking":"t"},{"type":"text","text":""},{"type":"text","text":"b"}]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":1,"output_tokens":2}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("command-r-plus")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range res.Content {
		switch p := c.(type) {
		case types.TextContent:
			got = append(got, "text:"+p.Text)
		case types.ReasoningContent:
			got = append(got, "reasoning:"+p.Text)
		}
	}
	if strings.Join(got, ",") != "text:a,reasoning:t,text:b" || res.Text != "ab" {
		t.Fatalf("content = %v, text = %q", got, res.Text)
	}
}
