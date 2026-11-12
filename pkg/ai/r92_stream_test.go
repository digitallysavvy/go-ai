package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

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
