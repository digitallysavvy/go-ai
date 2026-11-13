package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Round 92 Appendix A citations for the 2ebc78b654 delta (stream area):
// generate-text/array-output-stream.test.ts 47, 84, 119, 145, 161, 186 are
// ported below; util/array-output-stream.test.ts 7, 46, 57, 71 are ported
// by TestR91streamArrayStreamParserChunks / ...WithholdAndFallback (81 is the
// legacy streamObject parser, TestR91genStreamObjectArrayIncremental).
// to-ui-message-stream.test.ts 262, 308 are TestR91streamUIInvalidToolCallOnErrorOnce;
// stream-text.test.ts 27044 is TestR72StreamUIInvalidToolCall.

// r92streamTextChunks is TS array-output-stream.test.ts modelFor(text): one
// text block streamed one character per delta, then finish.
func r92streamTextChunks(text string) []provider.StreamChunk {
	chunks := []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypeTextStart, ID: "1"}}
	for _, r := range text {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: string(r)})
	}
	return append(chunks, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage})
}

func r92streamElements[E any](t *testing.T, r *StreamTextResult, output Output[[]E, []E]) []E {
	t.Helper()
	var out []E
	for e := range ElementStreamWithOutput(r, output) {
		out = append(out, e.Element)
	}
	return out
}

func r92streamJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var r92streamNumber = map[string]interface{}{"type": "number"}

// TS array-output-stream.test.ts 47 (streamText): validates each completed
// element once, then validates the final output; the element stream reads the
// partial outputs the stream published instead of reparsing the text.
func TestR92streamElementStreamValidatesEachElementOnce(t *testing.T) {
	cs := &r91streamCountingSchema{inner: schema.NewJSONSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"},
	})}
	values := make([]map[string]interface{}, 20)
	for i := range values {
		values[i] = map[string]interface{}{"value": fmt.Sprintf("value %d", i)}
	}
	text := r92streamJSON(map[string]interface{}{"elements": values})
	output := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{ElementSchema: cs})
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(text)), Prompt: "Return elements", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if got := r92streamJSON(r92streamElements(t, r, output)); got != r92streamJSON(values) {
		t.Fatalf("elements = %s", got)
	}
	_ = r.Err()
	if got := r92streamJSON(r.Output()); got != r92streamJSON(values) {
		t.Fatalf("output = %s", got)
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.calls != len(values)*2 {
		t.Fatalf("validations = %d, want %d", cs.calls, len(values)*2)
	}
}

// TS array-output-stream.test.ts 84 (streamText): the incremental parser hands
// its published state back to the stateless parser without a duplicate
// partial, and the text stream keeps the whole text.
func TestR92streamArrayHandsBackPublishedState(t *testing.T) {
	newResult := func() (*StreamTextResult, Output[[]float64, []float64]) {
		output := ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)})
		r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(`{"elements":[1,2],"other":3}`)), Prompt: "Return elements", Output: output})
		if err != nil {
			t.Fatal(err)
		}
		return r, output
	}
	r, _ := newResult()
	_ = r.Err()
	var partials []interface{}
	for i := 0; ; i++ {
		v, ok, _ := r.partials.next(i, nil)
		if !ok {
			break
		}
		partials = append(partials, v)
	}
	if got := r92streamJSON(partials); got != `[[],[1],[1,2]]` {
		t.Fatalf("partials = %s", got)
	}
	r, output := newResult()
	if got := r92streamJSON(r92streamElements(t, r, output)); got != `[1,2]` {
		t.Fatalf("elements = %s", got)
	}
	r, _ = newResult()
	var text strings.Builder
	for _, c := range r91streamDrain(r.Stream()) {
		if c.Type == provider.ChunkTypeText {
			text.WriteString(c.Text)
		}
	}
	if text.String() != `{"elements":[1,2],"other":3}` {
		t.Fatalf("text = %q", text.String())
	}
}

// TS array-output-stream.test.ts 119 (streamText): a nonstandard envelope
// keeps working through the stateless parser.
func TestR92streamElementStreamNonstandardEnvelope(t *testing.T) {
	elem := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": r92streamNumber}, "required": []interface{}{"value"}})
	output := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{ElementSchema: elem})
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(`{"other": [], "elements": [{"value":1}, {"value":2}]}`)), Prompt: "Return elements", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if got := r92streamJSON(r92streamElements(t, r, output)); got != `[{"value":1},{"value":2}]` {
		t.Fatalf("elements = %s", got)
	}
}

// TS array-output-stream.test.ts 145: concurrent streams that reuse one
// ArrayOutput keep their own parser state.
func TestR92streamElementStreamConcurrentReuse(t *testing.T) {
	output := ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)})
	var wg sync.WaitGroup
	got := make([]string, 2)
	for i, v := range []int{1, 2} {
		r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(fmt.Sprintf(`{"elements":[%d]}`, v))), Prompt: "Return elements", Output: output})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(i int, r *StreamTextResult) {
			defer wg.Done()
			var out []float64
			for e := range ElementStreamWithOutput(r, output) {
				out = append(out, e.Element)
			}
			got[i] = r92streamJSON(out)
		}(i, r)
	}
	wg.Wait()
	if got[0] != `[1]` || got[1] != `[2]` {
		t.Fatalf("elements = %v", got)
	}
}

// TS array-output-stream.test.ts 161: the element stream restarts with the
// stream retry (the failed attempt's text is not part of it).
func TestR92streamElementStreamResetsAfterFailedAttempt(t *testing.T) {
	failed := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: `{"elements":[1,`},
		{Type: provider.ChunkTypeError, Err: errors.New("retry"), Text: "retry"},
	}
	retries := 1
	output := ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, failed, r92streamTextChunks(`{"elements":[2]}`)), Prompt: "Return elements",
		Output: output, StreamRetries: &retries, OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r92streamJSON(r92streamElements(t, r, output)); got != `[2]` {
		t.Fatalf("elements = %s", got)
	}
	_ = r.Err()
	if got := r92streamJSON(r.Output()); got != `[2]` {
		t.Fatalf("output = %s", got)
	}
}

// r92streamOverriddenArray is a built-in array output whose partial parser is
// replaced (TS: output.parsePartialOutput = vi.fn(...)).
type r92streamOverriddenArray struct {
	*arrayOutput[float64]
	mu    sync.Mutex
	calls int
}

func (o *r92streamOverriddenArray) parsePartialOutput(context.Context, ParsePartialOutputOptions) (interface{}, bool, error) {
	o.mu.Lock()
	o.calls++
	o.mu.Unlock()
	return []float64{99}, true, nil
}

// TS array-output-stream.test.ts 186: an overridden partial parser on a
// built-in array output is honored (no incremental parser).
func TestR92streamElementStreamOverriddenPartialParser(t *testing.T) {
	inner := ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)}).(*arrayOutput[float64])
	output := &r92streamOverriddenArray{arrayOutput: inner}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(`{"elements":[1]}`)), Prompt: "Return elements", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if got := r92streamJSON(r92streamElements[float64](t, r, output)); got != `[99]` {
		t.Fatalf("elements = %s", got)
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.calls == 0 {
		t.Fatal("overridden parser not called")
	}
}

// TS createOutputTransformStream / createElementStreamTransform: elements come
// from the step's first text block only, also for ElementStream with its own
// schema (which reads the full stream).
func TestR92streamElementStreamFirstTextBlockOnly(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "a"},
		{Type: provider.ChunkTypeText, ID: "a", Text: `{"elements":[1`},
		{Type: provider.ChunkTypeTextStart, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "b", Text: `,2]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "a", Text: `]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "a"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
	}
	output := ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)})
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, chunks), Prompt: "x", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if got := r92streamJSON(r92streamElements(t, r, output)); got != `[1]` {
		t.Fatalf("ElementStreamWithOutput = %s", got)
	}
	r, err = StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, chunks), Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var out []float64
	for e := range ElementStream[float64](r, ElementStreamOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(r92streamNumber)}) {
		out = append(out, e.Element)
	}
	if got := r92streamJSON(out); got != `[1]` {
		t.Fatalf("ElementStream = %s", got)
	}
}

// TS stream-language-model-call.ts tool-input-start looks the tool up in the
// step's model tools (stepModelTools): a callee only a local tool caller can
// call gets no title, tool metadata or dynamic flag from its definition.
func TestR92streamToolInputStartUsesModelTools(t *testing.T) {
	callee := types.Tool{Name: "callee", Title: "Callee Title", Type: types.ToolTypeDynamic, Metadata: map[string]interface{}{"k": "v"},
		Parameters: map[string]interface{}{"type": "object"}}
	caller := types.Tool{Name: "caller", Parameters: map[string]interface{}{"type": "object"},
		ExperimentalToolCaller: &types.ToolCallerDefinition{Type: types.ToolCallerTypeLocal, Bind: func([]types.Tool) types.Tool {
			return types.Tool{Name: "caller", Parameters: map[string]interface{}{"type": "object"}}
		}}}
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "callee"}},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, chunks), Prompt: "x", Tools: []types.Tool{callee, caller},
		ExperimentalToolCallers: ExperimentalToolCallers{"callee": {"caller"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var start *types.ToolCall
	for _, c := range r91streamDrain(r.Stream()) {
		if c.Type == provider.ChunkTypeToolInputStart {
			start = c.ToolCall
		}
	}
	if start == nil {
		t.Fatal("no tool-input-start")
	}
	if start.Title != "" || start.ToolMetadata != nil || start.Dynamic {
		t.Fatalf("tool-input-start = %+v", *start)
	}
}

func r92streamValidateRaw(raw string) error {
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: raw})
	if res.Success {
		return nil
	}
	return res.Error
}

// TS validate-ui-messages.test.ts (11323f9894), "invalid messages and parts"
// 2465 / 2488 / 2524 / 2564 / 2581 / 2596 / 2619 / 2636 and "invalid JSON
// values in metadata" 2668 / 2705 / 2739 / 2772 / 2798: each case fails with
// one bounded ValidationError naming the first issue (Go reports the first
// issue as "<path>: <message>", and a non-finite number without its path:
// known differences). 2844 (unknown properties holding Infinity) is the
// known difference "Non-finite numbers in UI messages".
func TestR92streamValidateUIMessagesBoundedIssues(t *testing.T) {
	nest := func(v string, depth int) string {
		return strings.Repeat("[", depth) + v + strings.Repeat("]", depth)
	}
	many := strings.TrimSuffix(strings.Repeat("1e999,", 1000), ",")
	for name, tc := range map[string]struct{ raw, want string }{
		"2465 part schema":    {`[{"id":"1","role":"user","parts":[{"type":"text"}]}]`, "[0].parts[0]: Invalid input: expected string for text"},
		"2488 unknown type":   {`[{"id":"1","role":"user","parts":[{"type":"unknown"}]}]`, "[0].parts[0].type: " + uiPartTypeMessage},
		"2524 tool state":     {`[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"unknown"}]}]`, "[0].parts[0].state: " + uiToolStateMessage},
		"2564 first message":  {`[{"id":"1","role":"user","parts":[{"type":"text","text":"valid"}]},{},{}]`, "[1].id: "},
		"2581 first part":     {`[{"id":"1","role":"user","parts":[{"type":"text","text":"valid"},{},{}]}]`, "[0].parts[1].type: "},
		"2596 steering part":  {`[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"valid"},{},{}]}]}]`, "[0].parts[0]: parts[1]: "},
		"2619 first citation": {`[{"id":"1","role":"assistant","parts":[{"type":"text","text":"valid","citations":[{},{}]}]}]`, "citations[0].source"},
		"2668 provider meta":  {`[{"id":"1","role":"user","parts":[{"type":"text","text":"Hello","providerMetadata":{"provider":{"key":[1e999]}}}]}]`, uiInvalidJSONValueMessage},
		"2705 deep nested":    {`[{"id":"1","role":"user","parts":[{"type":"text","text":"Hello","providerMetadata":{"provider":{"key":` + nest("1e999", 300) + `}}}]}]`, uiInvalidJSONValueMessage},
		"2739 many values":    {`[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"input-available","input":{},"callProviderMetadata":{"provider":{"key":` + nest(many, 100) + `}}}]}]`, uiInvalidJSONValueMessage},
		"2772 tool metadata":  {`[{"id":"1","role":"assistant","parts":[{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-available","input":{},"toolMetadata":{"key":[[1e999]]}}]}]`, uiInvalidJSONValueMessage},
		"2798 citation meta":  {`[{"id":"1","role":"assistant","parts":[{"type":"text","text":"Hello","citations":[{"source":{"type":"source","sourceType":"url","id":"source-1","url":"https://example.com","providerMetadata":{"provider":{"key":[1e999]}}}}]}]}]`, uiInvalidJSONValueMessage},
	} {
		err := r92streamValidateRaw(tc.raw)
		var verr *providererrors.ValidationError
		if !errors.As(err, &verr) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v (%T), want %q", name, err, err, tc.want)
			continue
		}
		if len(err.Error()) > len(tc.raw)+5000 {
			t.Errorf("%s: error length %d for input %d", name, len(err.Error()), len(tc.raw))
		}
	}
}

// TS harness-stream-text-result.ts createOutputTransformStream: with an
// output, the turn's first text block feeds the partial output (not reset at
// step boundaries), its deltas are published with each changed partial and
// flushed at text-end, and other blocks pass through.
func TestR92streamExternalOutputStage(t *testing.T) {
	answer := ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	})})
	finishStep := provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	run := func(parts []provider.StreamChunk) (*StreamTextResult, []string) {
		r := NewStreamTextResultFromParts(context.Background(), &r92streamParts{parts: parts}, ExternalStreamOptions{Output: answer, TerminalFinishClosesTurn: true})
		var deltas []string
		for _, c := range r91streamDrain(r.Stream()) {
			if c.Type == provider.ChunkTypeText {
				deltas = append(deltas, c.ID+":"+c.Text)
			}
		}
		_ = r.Err()
		return r, deltas
	}
	r, deltas := run([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "s"},
		{Type: provider.ChunkTypeText, ID: "s", Text: `{"answer":"yes"`},
		{Type: provider.ChunkTypeText, ID: "s", Text: ` `},
		{Type: provider.ChunkTypeText, ID: "s", Text: `}`},
		{Type: provider.ChunkTypeTextEnd, ID: "s"},
		finishStep, finish,
	})
	if got := r92streamJSON(deltas); got != `["s:{\"answer\":\"yes\"","s: }"]` {
		t.Fatalf("deltas = %s", got)
	}
	if got := r92streamJSON(r.PartialOutput()); got != `{"answer":"yes"}` {
		t.Fatalf("partial = %s", got)
	}
	// A tool step's text block comes first: the final step's JSON block is
	// not the turn's first text block, so it publishes no partial.
	r, _ = run([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "a"},
		{Type: provider.ChunkTypeText, ID: "a", Text: "Let me check."},
		{Type: provider.ChunkTypeTextEnd, ID: "a"},
		finishStep,
		{Type: provider.ChunkTypeTextStart, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "b", Text: `{"answer":"yes"}`},
		{Type: provider.ChunkTypeTextEnd, ID: "b"},
		finishStep, finish,
	})
	if r.PartialOutput() != nil {
		t.Fatalf("partial = %#v, want none", r.PartialOutput())
	}
	if got := r92streamJSON(r.Output()); got != `{"answer":"yes"}` {
		t.Fatalf("output = %s", got)
	}
}

// r92streamParts is a TextStream over fixed parts.
type r92streamParts struct {
	parts []provider.StreamChunk
	i     int
}

func (s *r92streamParts) Next() (*provider.StreamChunk, error) {
	if s.i >= len(s.parts) {
		return nil, io.EOF
	}
	c := s.parts[s.i]
	s.i++
	return &c, nil
}
func (s *r92streamParts) Err() error   { return nil }
func (s *r92streamParts) Close() error { return nil }
