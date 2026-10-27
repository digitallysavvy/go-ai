package libfx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func newBareTurn() *Turn {
	return &Turn{changed: make(chan struct{}), controllers: map[int]context.CancelFunc{}, done: make(chan struct{}), toolNames: map[string]string{}, started: map[string]bool{}}
}

func TestHostToolContent(t *testing.T) {
	isErr := true
	for _, c := range []struct {
		in   any
		want string
		rich bool
	}{
		{"plain", "plain", false},
		{nil, "null", false},
		{map[string]any{"a": 1}, `{"a":1}`, false},
		{&TypedToolResult{Type: "libfx.tool-result", Text: "t", Images: []ToolImage{{Type: "image", MimeType: "image/png", Data: "AQID"}}, IsError: &isErr}, `{"text":"t","images":[{"type":"image","mimeType":"image/png","data":"AQID"}]}`, true},
	} {
		got, rich, err := hostToolContent(c.in)
		if err != nil || got != c.want || rich != c.rich {
			t.Errorf("%v: %q %v %v", c.in, got, rich, err)
		}
	}
	if _, _, err := hostToolContent(TypedToolResult{Type: "libfx.tool-result", Images: []ToolImage{{Type: "image"}}}); err == nil {
		t.Error("an image without a mime type was accepted")
	}
	// libfx hostToolContent: images past the limit without a sourceRef fail
	// as "tool images exceed the result limit"; a referenced image's data is
	// dropped first, also when only the encoded result is too large.
	big := strings.Repeat("A", 3<<20)
	img := func(ref string) ToolImage {
		return ToolImage{Type: "image", MimeType: "image/png", Data: big, SourceRef: ref}
	}
	if _, _, err := hostToolContent(TypedToolResult{Type: "libfx.tool-result", Images: []ToolImage{img(""), img(""), img("")}}); err == nil || err.Error() != "tool images exceed the result limit" {
		t.Errorf("unreferenced images over the limit = %v", err)
	}
	got, _, err := hostToolContent(TypedToolResult{Type: "libfx.tool-result", Images: []ToolImage{img(""), img("ref://b"), img("ref://c")}})
	if err != nil || strings.Count(got, big) != 1 || !strings.Contains(got, `"sourceRef":"ref://c"`) {
		t.Errorf("referenced images over the limit = %d bytes, %v", len(got), err)
	}
	two := strings.Repeat("A", 4<<20-100)
	got, _, err = hostToolContent(TypedToolResult{Type: "libfx.tool-result", Text: strings.Repeat("t", 200), Images: []ToolImage{{Type: "image", MimeType: "image/png", Data: two}, {Type: "image", MimeType: "image/png", Data: two, SourceRef: "ref://x"}}})
	if err != nil || strings.Count(got, two) != 1 {
		t.Errorf("an encoded result over the limit kept referenced data: %d bytes, %v", len(got), err)
	}
	if got, _, _ := hostToolContent(TypedToolResult{Type: "libfx.tool-result", Text: "<b>&", Images: []ToolImage{}}); got != `{"text":"<b>&","images":[]}` {
		t.Errorf("content is not JSON.stringify output: %s", got)
	}
}

func TestNormalizeHostTools(t *testing.T) {
	exec := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	schema := map[string]any{"type": "object"}
	if _, _, err := normalizeHostTools([]Tool{{Name: "bad name", Execute: exec, InputSchema: schema}}); err == nil {
		t.Error("invalid name accepted")
	}
	if _, _, err := normalizeHostTools([]Tool{{Name: "a", Execute: exec, InputSchema: schema}, {Name: "a", Execute: exec, InputSchema: schema}}); err == nil {
		t.Error("duplicate accepted")
	}
	if _, _, err := normalizeHostTools([]Tool{{Name: "a", Execute: exec, InputSchema: []any{}}}); err == nil {
		t.Error("array schema accepted")
	}
	many := make([]Tool, 65)
	for i := range many {
		many[i] = Tool{Name: "t" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Execute: exec, InputSchema: schema}
	}
	if _, _, err := normalizeHostTools(many); err == nil {
		t.Error("65 tools accepted")
	}
	d, _, err := normalizeHostTools([]Tool{{Name: "p", ProviderExecuted: true}, {Name: "x", Description: "d", Execute: exec, InputSchema: schema, Idempotent: true}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(d)
	if string(got) != `[{"name":"p","providerExecuted":true},{"description":"d","idempotent":true,"inputSchema":{"type":"object"},"name":"x"}]` {
		t.Errorf("descriptors = %s", got)
	}
}

func TestPreparePromptLimits(t *testing.T) {
	prompt, images, err := preparePrompt([]PromptBlock{{Type: "text", Text: "hi"}, {Type: "image", Data: "AQID", MimeType: "image/png"}})
	if err != nil || len(images) != 1 || string(images[0]) != "\x01\x02\x03" || prompt[1]["mimeType"] != "image/png" {
		t.Errorf("prepare = %v %v %v", prompt, images, err)
	}
	if _, _, err := preparePrompt([]PromptBlock{{Type: "image", Data: "AQID"}}); err == nil {
		t.Error("image without mime type accepted")
	}
	nine := make([]PromptBlock, 9)
	for i := range nine {
		nine[i] = PromptBlock{Type: "image", Data: "AQID", MimeType: "image/png"}
	}
	if _, _, err := preparePrompt(nine); err == nil {
		t.Error("nine images accepted")
	}
	if _, _, err := preparePrompt([]PromptBlock{{Type: "audio"}}); err == nil {
		t.Error("unknown block accepted")
	}
}

func TestTurnEventNormalization(t *testing.T) {
	tr := newBareTurn()
	cases := []struct {
		update string
		want   Event
		ok     bool
	}{
		{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}`, Event{Type: "text_delta", Delta: "hi"}, true},
		{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"[context] x"}}`, Event{}, false},
		{`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"why"}}`, Event{Type: "reasoning_delta", Delta: "why"}, true},
		{`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"steer"},"_meta":{"fx":{"steering":{"inputId":"in_1"}}}}`, Event{Type: "user_message", Text: "steer", ID: "in_1"}, true},
		{`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"bash","rawInput":{"command":"ls"}}`, Event{Type: "tool_start", ID: "c1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)}, true},
		{`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"bash"}`, Event{}, false},
		{`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress"}`, Event{}, false},
	}
	for _, c := range cases {
		got, ok := tr.eventFor(json.RawMessage(c.update))
		if ok != c.ok || got.Type != c.want.Type || got.Delta != c.want.Delta || got.Text != c.want.Text || got.ID != c.want.ID || got.Name != c.want.Name || string(got.Input) != string(c.want.Input) {
			t.Errorf("%s: %+v %v", c.update, got, ok)
		}
	}
	end, ok := tr.eventFor(json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"failed","content":[{"content":{"type":"text","text":"boom"}}]}`))
	if !ok || end.Type != "tool_end" || end.Name != "bash" || !end.IsError || end.Content == nil || *end.Content != "boom" {
		t.Errorf("tool_end = %+v", end)
	}
}

// TestTurnPushBackpressure fills the unread queue: the next push blocks
// until the consumer reads, and a cancel releases it.
func TestTurnPushBackpressure(t *testing.T) {
	tr := newBareTurn()
	update := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"x"}}`)
	for i := 0; i < maxUnreadEvents; i++ {
		tr.push(context.Background(), update, 10)
	}
	pushed := make(chan struct{})
	go func() {
		tr.push(context.Background(), update, 10)
		close(pushed)
	}()
	select {
	case <-pushed:
		t.Fatal("push did not block on a full queue")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := tr.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-pushed
	// A big update waits for an empty queue, and cancel releases it.
	blocked := make(chan struct{})
	go func() {
		tr.push(context.Background(), update, maxUnreadEventBytes)
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("oversized push did not wait")
	case <-time.After(50 * time.Millisecond):
	}
	tr.mu.Lock()
	tr.cancelled = true
	tr.signalLocked()
	tr.mu.Unlock()
	<-blocked
	tr.mu.Lock()
	tr.finished = true
	tr.queue = nil
	tr.signalLocked()
	tr.mu.Unlock()
	if _, err := tr.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("next after finish = %v", err)
	}
}

// libfx normalizeImageSourceRef: a tool image's sourceRef is at most 512
// bytes of UTF-8 without ASCII controls, or the result fails.
func TestHostToolContentChecksSourceRef(t *testing.T) {
	result := func(ref string) TypedToolResult {
		return TypedToolResult{Type: "libfx.tool-result", Images: []ToolImage{{Type: "image", MimeType: "image/png", SourceRef: ref}}}
	}
	for ref, want := range map[string]string{
		strings.Repeat("r", 513): "tool image sourceRef exceeds the 512 byte libfx limit",
		"ref\x00x":               "tool image sourceRef must be valid UTF-8 without ASCII controls",
		"ref\x7f":                "tool image sourceRef must be valid UTF-8 without ASCII controls",
		"ref\xff":                "tool image sourceRef must be valid UTF-8 without ASCII controls",
	} {
		if _, _, err := hostToolContent(result(ref)); err == nil || err.Error() != want {
			t.Errorf("sourceRef %q: %v, want %q", ref, err, want)
		}
	}
	if got, _, err := hostToolContent(result(strings.Repeat("r", 512))); err != nil || !strings.Contains(got, `"sourceRef"`) {
		t.Errorf("a 512-byte sourceRef: %s, %v", got, err)
	}
}
