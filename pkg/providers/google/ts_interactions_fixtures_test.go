package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

// The TS Interactions recordings (packages/google/src/interactions/__fixtures__,
// copied to testdata/ts/interactions) driven through the Go model.

func interactionsFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ts", "interactions", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func interactionsModel(t *testing.T, fixture string, capture *map[string]interface{}) (*InteractionsLanguageModel, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, capture)
		}
		if strings.HasSuffix(fixture, ".chunks.txt") {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, l := range strings.Split(string(interactionsFixture(t, fixture)), "\n") {
				if strings.TrimSpace(l) != "" {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(interactionsFixture(t, fixture))
	}))
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	return m.(*InteractionsLanguageModel), srv.Close
}

func weatherToolI() types.Tool {
	return types.Tool{Name: "getWeather", Description: "Get the current weather in a location", Parameters: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}, "required": []interface{}{"location"},
	}}
}

func interactionsGenerate(t *testing.T, fixture string, opts *provider.GenerateOptions, capture *map[string]interface{}) *types.GenerateResult {
	t.Helper()
	m, closeFn := interactionsModel(t, fixture, capture)
	defer closeFn()
	if opts == nil {
		opts = &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello, how are you?"}}
	}
	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func interactionsStreamFixture(t *testing.T, fixture string, opts *provider.GenerateOptions) []*provider.StreamChunk {
	t.Helper()
	m, closeFn := interactionsModel(t, fixture, nil)
	defer closeFn()
	if opts == nil {
		opts = &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello, how are you?"}}
	}
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return drainAll(t, s)
}

func metaGoogle(t *testing.T, raw interface{}) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("metadata %s: %v", b, err)
	}
	return m["google"]
}

const basicInteractionID = "v1_ChdTbXNIYXFyUEV0ZUttdGtQNXVqVHdRRRIXU21zSGFxclBFdGVLbXRrUDV1alR3UUU"

// TS "basic text": content, finishReason, usage, providerMetadata.
func TestInteractionsFixtureBasicGenerate(t *testing.T) {
	var body map[string]interface{}
	res := interactionsGenerate(t, "basic.json", nil, &body)
	if len(res.Content) != 2 {
		t.Fatalf("content = %+v", res.Content)
	}
	rc, ok := res.Content[0].(types.ReasoningContent)
	if !ok || rc.Text != "" || !strings.HasPrefix(rc.Signature, "CqwBAQw51sfg") {
		t.Fatalf("reasoning = %+v", res.Content[0])
	}
	rm := metaGoogle(t, rc.ProviderMetadata)
	if rm["interactionId"] != basicInteractionID || rm["signature"] != rc.Signature {
		t.Fatalf("reasoning metadata = %v", rm)
	}
	tc, ok := res.Content[1].(types.TextContent)
	if !ok || tc.Text != "Hello! I'm doing well, thank you for asking.\n\nHow are you today?" || metaGoogle(t, tc.ProviderMetadata)["interactionId"] != basicInteractionID {
		t.Fatalf("text = %+v", res.Content[1])
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "completed" {
		t.Fatalf("finish = %v/%q", res.FinishReason, res.RawFinishReason)
	}
	pm := metaGoogle(t, res.ProviderMetadata)
	if pm["interactionId"] != basicInteractionID || pm["serviceTier"] != "standard" {
		t.Fatalf("providerMetadata = %v", pm)
	}
	// TS snapshot: input 7 (noCache 7, cacheRead 0), output text 19 + reasoning 32 = 51.
	u := res.Usage
	if *u.InputTokens != 7 || *u.InputDetails.NoCacheTokens != 7 || *u.InputDetails.CacheReadTokens != 0 ||
		*u.OutputTokens != 51 || *u.OutputDetails.TextTokens != 19 || *u.OutputDetails.ReasoningTokens != 32 {
		t.Fatalf("usage = %+v / %+v / %+v", u, u.InputDetails, u.OutputDetails)
	}
	rawJSON, _ := json.Marshal(u.Raw)
	if string(rawJSON) != `{"input_tokens_by_modality":[{"modality":"text","tokens":7}],"total_cached_tokens":0,"total_input_tokens":7,"total_output_tokens":19,"total_thought_tokens":32,"total_tokens":58,"total_tool_use_tokens":0}` {
		t.Fatalf("usage.raw = %s (TS passes the API object through)", rawJSON)
	}
	if body["model"] != "gemini-2.5-flash" {
		t.Fatalf("request body = %v", body)
	}
}

// TS "streams text-start, reasoning, text-delta, text-end, finish parts".
func TestInteractionsFixtureBasicStream(t *testing.T) {
	chunks := interactionsStreamFixture(t, "basic.chunks.txt", nil)
	const id = "v1_ChdUR3NIYXVyQkFlYVA2ZGtQajZERThBVRIXVEdzSGF1ckJBZWFQNmRrUGo2REU4QVU"
	var kinds []string
	for _, c := range chunks {
		k := string(c.Type)
		if n := len(kinds); n > 0 && kinds[n-1] == k && k == "response-metadata" {
			continue
		}
		kinds = append(kinds, k)
	}
	if got := strings.Join(kinds, ","); got != "stream-start,response-metadata,reasoning-start,reasoning-end,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeReasoningStart:
			if c.ID != id+":0" {
				t.Fatalf("reasoning id = %q", c.ID)
			}
		case provider.ChunkTypeReasoningEnd:
			if m := metaGoogle(t, c.ProviderMetadata); m["interactionId"] != id || !strings.HasPrefix(fmt.Sprint(m["signature"]), "CiQBDDnW") {
				t.Fatalf("reasoning-end metadata = %v", m)
			}
		case provider.ChunkTypeTextStart, provider.ChunkTypeText:
			if c.ID != id+":1" {
				t.Fatalf("text id = %q", c.ID)
			}
		case provider.ChunkTypeTextEnd:
			if metaGoogle(t, c.ProviderMetadata)["interactionId"] != id {
				t.Fatalf("text-end metadata = %s", c.ProviderMetadata)
			}
		case provider.ChunkTypeFinish:
			pm := metaGoogle(t, c.ProviderMetadata)
			u := c.Usage
			if c.FinishReason != types.FinishReasonStop || c.RawFinishReason != "completed" || pm["serviceTier"] != "standard" ||
				*u.InputTokens != 7 || *u.OutputTokens != 141 || *u.OutputDetails.TextTokens != 26 || *u.OutputDetails.ReasoningTokens != 115 {
				t.Fatalf("finish = %v/%q meta %v usage %+v %+v", c.FinishReason, c.RawFinishReason, pm, u, u.OutputDetails)
			}
		}
	}
}

// TS "tool calling (multi-step flow)": step 1 asks for the tool, step 2 answers.
func TestInteractionsFixtureToolCallSteps(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "What is the weather in San Francisco right now?"}, Tools: []types.Tool{weatherToolI()}}
	res := interactionsGenerate(t, "tool-call-step1.json", opts, nil)
	if res.FinishReason != types.FinishReasonToolCalls || res.RawFinishReason != "requires_action" || len(res.ToolCalls) != 1 {
		t.Fatalf("step1: finish=%v/%q calls=%d", res.FinishReason, res.RawFinishReason, len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.ID != "zggxzq8r" || call.ToolName != "getWeather" || call.Arguments["location"] != "San Francisco" {
		t.Fatalf("call = %+v", call)
	}
	var found bool
	for _, c := range res.Content {
		if tc, ok := c.(types.ToolCallContent); ok {
			found = true
			if metaGoogle(t, tc.ProviderMetadata)["interactionId"] != "v1_ChdUMnNIYXVxU0lJX2lxdHNQX2FicXVBWRIXVDJzSGF1cVNJSV9pcXRzUF9hYnF1QVk" {
				t.Fatalf("tool call metadata = %s", tc.ProviderMetadata)
			}
		}
	}
	if !found {
		t.Fatal("no tool-call content")
	}

	chunks := interactionsStreamFixture(t, "tool-call-step1.chunks.txt", opts)
	var seq []string
	var input strings.Builder
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeFinish:
			seq = append(seq, string(c.Type))
			if c.Type == provider.ChunkTypeToolInputDelta {
				input.WriteString(c.Text)
			}
			if c.Type == provider.ChunkTypeFinish && (c.FinishReason != types.FinishReasonToolCalls || c.RawFinishReason != "requires_action") {
				t.Fatalf("finish = %v/%q", c.FinishReason, c.RawFinishReason)
			}
		}
	}
	if strings.Join(seq, ",") != "tool-input-start,tool-input-delta,tool-input-end,tool-call,finish" || input.String() != `{"location":"San Francisco"}` {
		t.Fatalf("seq=%v input=%q", seq, input.String())
	}

	res = interactionsGenerate(t, "tool-call-step2.json", opts, nil)
	if res.FinishReason != types.FinishReasonStop || len(res.ToolCalls) != 0 || res.Text == "" {
		t.Fatalf("step2: %v text=%q", res.FinishReason, res.Text)
	}
}

// TS multi-turn stateful / stateless fixtures (turn 2 request bodies).
func TestInteractionsFixtureMultiTurn(t *testing.T) {
	const turn1ID = "v1_ChdWV3NIYXNYZEc5S19xdHNQcmVYeG1BRRIXVldzSGFzWGRHOUtfcXRzUHJlWHhtQUU"
	res := interactionsGenerate(t, "multi-turn-stateful-turn1.json", &provider.GenerateOptions{Prompt: types.Prompt{Text: "What are the three largest cities in Spain?"}}, nil)
	if metaGoogle(t, res.ProviderMetadata)["interactionId"] != turn1ID || !strings.Contains(res.Text, "Madrid") || !strings.Contains(res.Text, "Barcelona") || !strings.Contains(res.Text, "Valencia") {
		t.Fatalf("turn1: %v / %q", res.ProviderMetadata, res.Text)
	}

	user := func(text string) types.Message {
		return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
	}
	cities := "The three largest cities in Spain are Madrid, Barcelona, and Valencia."

	// Stateful turn 2: assistant turn compacted away, previous_interaction_id sent.
	var body map[string]interface{}
	res = interactionsGenerate(t, "multi-turn-stateful-turn2.json", &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			user("What are the three largest cities in Spain?"),
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"interactionId": turn1ID, "signature": "thought-sig-from-turn-1"}}},
				types.TextContent{Text: cities, ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"interactionId": turn1ID}}},
			}},
			user("What is the most famous landmark in the second one?"),
		}},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"previousInteractionId": turn1ID}},
	}, &body)
	if body["previous_interaction_id"] != turn1ID {
		t.Fatalf("previous_interaction_id = %v", body["previous_interaction_id"])
	}
	in, _ := json.Marshal(body["input"])
	if string(in) != `[{"content":[{"text":"What are the three largest cities in Spain?","type":"text"}],"type":"user_input"},{"content":[{"text":"What is the most famous landmark in the second one?","type":"text"}],"type":"user_input"}]` {
		t.Fatalf("stateful input = %s", in)
	}
	if !strings.Contains(res.Text, "Barcelona") || !strings.Contains(res.Text, "Sagrada Fam") {
		t.Fatalf("turn2 text = %q", res.Text)
	}

	// Stateless turn 1 and turn 2: store=false, full history verbatim.
	body = nil
	interactionsGenerate(t, "multi-turn-stateless-turn1.json", &provider.GenerateOptions{
		Prompt:          types.Prompt{Messages: []types.Message{user("What are the three largest cities in Spain?")}},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"store": false}},
	}, &body)
	if body["store"] != false {
		t.Fatalf("turn1 store = %v", body["store"])
	}
	body = nil
	res = interactionsGenerate(t, "multi-turn-stateless-turn2.json", &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			user("What are the three largest cities in Spain?"),
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: cities}}},
			user("What is the most famous landmark in the second one?"),
		}},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"store": false}},
	}, &body)
	if body["store"] != false || body["previous_interaction_id"] != nil {
		t.Fatalf("stateless body = %v", body)
	}
	in, _ = json.Marshal(body["input"])
	if string(in) != `[{"content":[{"text":"What are the three largest cities in Spain?","type":"text"}],"type":"user_input"},{"content":[{"text":"`+cities+`","type":"text"}],"type":"model_output"},{"content":[{"text":"What is the most famous landmark in the second one?","type":"text"}],"type":"user_input"}]` {
		t.Fatalf("stateless input = %s", in)
	}
	if len(res.Warnings) != 0 || !strings.Contains(res.Text, "Sagrada Fam") {
		t.Fatalf("warnings=%v text=%q", res.Warnings, res.Text)
	}
}

// TS "google_search" fixture: provider-executed call/result parts and sources.
func TestInteractionsFixtureGoogleSearch(t *testing.T) {
	var body map[string]interface{}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello, how are you?"}, Tools: []types.Tool{gemini.GoogleSearchTool()}}
	res := interactionsGenerate(t, "google-search.json", opts, &body)
	if b, _ := json.Marshal(body["tools"]); string(b) != `[{"type":"google_search"}]` {
		t.Fatalf("tools = %s", b)
	}
	var call, result bool
	urls := 0
	for _, c := range res.Content {
		switch p := c.(type) {
		case types.ToolCallContent:
			call = call || (p.ToolName == "google_search" || p.ToolName == "google.google_search") && p.ProviderExecuted
		case types.ToolResultContent:
			result = result || p.ToolName == "google_search" || p.ToolName == "google.google_search"
		case types.SourceContent:
			if p.SourceType == "url" {
				urls++
				if !strings.HasPrefix(p.URL, "http") {
					t.Fatalf("source url = %q", p.URL)
				}
			}
		}
	}
	if !call || !result || urls == 0 || res.FinishReason != types.FinishReasonStop {
		t.Fatalf("call=%v result=%v urls=%d finish=%v content=%+v", call, result, urls, res.FinishReason, res.Content)
	}
}

// TS "image output": a file part with image data and the interaction id.
func TestInteractionsFixtureImageOutput(t *testing.T) {
	var body map[string]interface{}
	imageOpts := func(extra map[string]interface{}) *provider.GenerateOptions {
		google := map[string]interface{}{"responseModalities": []interface{}{"image"}}
		for k, v := range extra {
			google[k] = v
		}
		return &provider.GenerateOptions{Prompt: types.Prompt{Text: "Generate an image of a comic cat in a spaceship."}, ProviderOptions: map[string]interface{}{"google": google}}
	}
	res := interactionsGenerate(t, "image-output.json", imageOpts(nil), &body)
	var file *types.GeneratedFileContent
	for _, c := range res.Content {
		if f, ok := c.(types.GeneratedFileContent); ok {
			file = &f
		}
	}
	if file == nil || !strings.HasPrefix(file.MediaType, "image/") || len(file.Data) == 0 || !strings.HasPrefix(fmt.Sprint(metaGoogle(t, file.ProviderMetadata)["interactionId"]), "v1_") {
		t.Fatalf("file = %+v", file)
	}
	if b, _ := json.Marshal(body["response_modalities"]); string(b) != `["image"]` {
		t.Fatalf("response_modalities = %s", b)
	}

	body = nil
	res = interactionsGenerate(t, "image-output-modify.json", imageOpts(map[string]interface{}{"previousInteractionId": "v1_prev-turn"}), &body)
	file = nil
	for _, c := range res.Content {
		if f, ok := c.(types.GeneratedFileContent); ok {
			file = &f
		}
	}
	if file == nil || file.MediaType != "image/jpeg" || body["previous_interaction_id"] != "v1_prev-turn" {
		t.Fatalf("modify: file=%+v prev=%v", file, body["previous_interaction_id"])
	}

	// The stream variants carry the image as a file chunk.
	for _, fx := range []string{"image-output.chunks.txt", "image-output-modify.chunks.txt"} {
		files := 0
		for _, c := range interactionsStreamFixture(t, fx, imageOpts(nil)) {
			if c.Type == provider.ChunkTypeFile {
				files++
			}
		}
		if files != 1 {
			t.Fatalf("%s: file chunks = %d", fx, files)
		}
	}
}

// TS "structured output (responseFormat)": JSON text with reasoning first.
func TestInteractionsFixtureStructuredOutput(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello, how are you?"}, ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}, "age": map[string]interface{}{"type": "number"}},
	}}}
	res := interactionsGenerate(t, "structured-output.json", opts, nil)
	if len(res.Content) != 2 {
		t.Fatalf("content = %+v", res.Content)
	}
	if _, ok := res.Content[0].(types.ReasoningContent); !ok {
		t.Fatalf("content[0] = %+v", res.Content[0])
	}
	if tc, ok := res.Content[1].(types.TextContent); !ok || tc.Text != `{"name":"John Doe","age":30}` {
		t.Fatalf("content[1] = %+v", res.Content[1])
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

// Every Interactions stream fixture finishes cleanly through the Go stream.
func TestInteractionsEveryStreamFixtureFinishes(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "ts", "interactions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".chunks.txt") {
			continue
		}
		chunks := interactionsStreamFixture(t, e.Name(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{weatherToolI(), gemini.GoogleSearchTool()}})
		if len(chunks) == 0 || chunks[0].Type != provider.ChunkTypeStreamStart {
			t.Fatalf("%s: first chunk = %+v", e.Name(), chunks)
		}
		fin := chunks[len(chunks)-1]
		if fin.Type != provider.ChunkTypeFinish || fin.FinishReason == types.FinishReasonError {
			t.Fatalf("%s: last chunk = %+v", e.Name(), fin)
		}
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				t.Fatalf("%s: error chunk %v", e.Name(), c.Err)
			}
		}
	}
}
