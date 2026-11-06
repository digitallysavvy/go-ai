package streaming

import (
	"io"

	"encoding/json"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r82provbTrackerPart renders a tracker chunk the way the TS stream part
// prints (type, id, toolName / delta / input).
func r82provbTrackerPart(c ToolCallChunk) string {
	switch c.Type {
	case provider.ChunkTypeToolInputStart:
		return "tool-input-start " + c.ToolCall.ID + " " + c.ToolCall.ToolName
	case provider.ChunkTypeToolInputDelta:
		return "tool-input-delta " + c.ToolCall.ID + " " + c.Text
	case provider.ChunkTypeToolInputEnd:
		return "tool-input-end " + c.ToolCall.ID
	case provider.ChunkTypeToolCall:
		meta := ""
		if c.ToolCall.ProviderMetadata != nil {
			b, _ := json.Marshal(c.ToolCall.ProviderMetadata)
			meta = " " + string(b)
		}
		return "tool-call " + c.ToolCall.ID + " " + c.ToolCall.ToolName + " " + c.ToolCall.RawArguments + meta
	case provider.ChunkTypeError:
		return "THROW " + c.Text
	}
	return string(c.Type)
}

// r82provbCompatDelta is one openai-compatible wire tool call delta.
type r82provbCompatDelta struct {
	Index    *int    `json:"index"`
	ID       *string `json:"id"`
	Function struct {
		Name      WireName `json:"name"`
		Arguments *string  `json:"arguments"`
	} `json:"function"`
	ExtraContent map[string]interface{} `json:"extra_content"`
}

// r82provbRunCompat feeds wire deltas through a tracker in the TS
// openai-compatible mode (pendingToolCalls) and returns the parts, ending
// with "finish" unless the tracker failed (TS threw).
func r82provbRunCompat(t *testing.T, wire string) []string {
	t.Helper()
	var deltas []r82provbCompatDelta
	if err := json.Unmarshal([]byte(wire), &deltas); err != nil {
		t.Fatal(err)
	}
	n := 0
	tr := NewStreamingToolCallTrackerWithIDGenerator(func() string {
		id := "gen-" + string(rune('0'+n))
		n++
		return id
	})
	tr.BufferNamelessIndexedCalls()
	var out []string
	for _, d := range deltas {
		td := ToolCallDelta{Index: d.Index, Name: d.Function.Name.Value, IDPresent: d.ID != nil, MetadataPresent: d.ExtraContent != nil,
			ProviderMetadata: openAICompatToolCallMetadata(d.ExtraContent, "p")}
		if d.ID != nil {
			td.ID = *d.ID
		}
		if d.Function.Arguments != nil {
			td.ArgumentsDelta, td.ArgumentsPresent = *d.Function.Arguments, true
		}
		if nonBlank(td.Name) == "" {
			td.NameBlank, td.NameAbsent = d.Function.Name.Present, !d.Function.Name.Present
		}
		for _, c := range tr.TrackDelta(td) {
			out = append(out, r82provbTrackerPart(c))
		}
		if tr.Failed() {
			return out
		}
	}
	for _, c := range tr.Flush() {
		out = append(out, r82provbTrackerPart(c))
	}
	if !tr.Failed() {
		out = append(out, "finish")
	}
	return out
}

// Each expected part list was produced by the TS tracker
// (streaming-tool-call-tracker.ts) behind the openai-compatible
// processToolCallDelta / flush() code at e64ed82f27.
func TestR82provbCompatPendingToolCallsMatchTS(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		want       []string
	}{
		{
			// flush() forwards the pending index 0 (unseen id c0) without a
			// name: the tracker throws before toolCallTracker.flush(), so the
			// named call at index 1 is never finalized.
			name: "never named pending throws before any tool call",
			wire: `[{"index":0,"id":"c0","function":{"arguments":"{}"}},{"index":1,"id":"c1","function":{"name":"t","arguments":"{}"}}]`,
			want: []string{"tool-input-start c1 t", "tool-input-delta c1 {}", "THROW Expected 'function.name' to be a string."},
		},
		{
			// A pending entry without an id resolves to the sole unfinished
			// call at flush and continues it.
			name: "id-less pending continues the sole unfinished call",
			wire: `[{"index":0,"function":{"arguments":"x"}},{"index":1,"id":"c1","function":{"name":"t","arguments":"{\"a\":"}}]`,
			want: []string{"tool-input-start c1 t", "tool-input-delta c1 {\"a\":", "tool-input-delta c1 x", "tool-input-end c1", "tool-call c1 t {\"a\":x", "finish"},
		},
		{
			// `id ?? null` keeps a present empty id: the later "real" id is
			// not adopted, and the blank id yields a generated one.
			name: "present empty id is kept",
			wire: `[{"index":0,"id":"","function":{"arguments":"{"}},{"index":0,"id":"real","function":{"name":"t","arguments":"}"}}]`,
			want: []string{"tool-input-start gen-0 t", "tool-input-delta gen-0 {}", "tool-input-end gen-0", "tool-call gen-0 t {}", "finish"},
		},
		{
			// A blank name forwards the index and is ignored; the next
			// nameless delta of that index goes to the tracker directly and
			// throws.
			name: "blank name forwards then nameless continuation throws",
			wire: `[{"index":0,"id":"a","function":{"name":"  ","arguments":"{"}},{"index":0,"function":{"arguments":"}"}}]`,
			want: []string{"THROW Expected 'function.name' to be a string."},
		},
		{
			// The first non-null extra_content wins even when it carries no
			// thought signature.
			name: "first present extra_content is kept",
			wire: `[{"index":0,"id":"a","extra_content":{"google":{}},"function":{"arguments":"{"}},{"index":0,"extra_content":{"google":{"thought_signature":"S"}},"function":{"name":"t","arguments":"}"}}]`,
			want: []string{"tool-input-start a t", "tool-input-delta a {}", "tool-input-end a", "tool-call a t {}", "finish"},
		},
		{
			name: "signature on the buffered delta reaches the call",
			wire: `[{"index":0,"id":"a","extra_content":{"google":{"thought_signature":"S"}},"function":{"arguments":"{"}},{"index":0,"function":{"name":"t","arguments":"}"}}]`,
			want: []string{"tool-input-start a t", "tool-input-delta a {}", "tool-input-end a", `tool-call a t {} {"p":{"thoughtSignature":"S"}}`, "finish"},
		},
		{
			// TS extracts metadata only when a call is created: a signature
			// on a continuation is ignored.
			name: "signature on a continuation is ignored",
			wire: `[{"index":0,"id":"a","function":{"name":"t","arguments":"{"}},{"index":0,"extra_content":{"google":{"thought_signature":"S"}},"function":{"arguments":"}"}}]`,
			want: []string{"tool-input-start a t", "tool-input-delta a {", "tool-input-delta a }", "tool-input-end a", "tool-call a t {}", "finish"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r82provbRunCompat(t, tc.wire)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parts:\n got %s\nwant %s", strings.Join(got, " | "), strings.Join(tc.want, " | "))
			}
		})
	}
}

// The OpenAICompatStream wires the wire id and extra_content presence into
// the pending buffer: a present empty id is kept as in TS.
func TestR82provbCompatStreamKeepsPresentEmptyID(t *testing.T) {
	body := `data: {"id":"x","choices":[{"delta":{"tool_calls":[{"index":0,"id":"","function":{"arguments":"{"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"real","function":{"name":"t","arguments":"}"}}]},"finish_reason":"tool_calls"}]}

`
	s := NewOpenAICompatStream(io.NopCloser(strings.NewReader(body)), mapTestFinishReason)
	var call *types.ToolCall
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolCall {
			call = c.ToolCall
		}
	}
	if call == nil || call.ID == "real" || call.ID == "" {
		t.Fatalf("tool call = %+v, want a generated id", call)
	}
}
