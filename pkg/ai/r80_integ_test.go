package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The getting-started EmailSummary example
// (docs/02-getting-started/01-navigating-the-library.mdx).
type r80integEmailSummary struct {
	Sender    string   `json:"sender"`
	Subject   string   `json:"subject"`
	KeyPoints []string `json:"key_points"`
	Priority  string   `json:"priority" validate:"required"`
}

func r80integTextModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

// The documented ObjectOutput struct-schema example works: v0.5.2 always
// failed with "No object generated: response did not match schema."
func TestR80integObjectOutputStructSchemaDocExample(t *testing.T) {
	ctx := context.Background()
	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  r80integTextModel(`{"sender":"a@b.c","subject":"Hi","key_points":["x"],"priority":"high"}`),
		Prompt: "Summarize this email: ...",
		Output: ObjectOutput[r80integEmailSummary](ObjectOutputOptions{
			Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Output.(r80integEmailSummary)
	if summary.Priority != "high" || summary.Sender != "a@b.c" || len(summary.KeyPoints) != 1 {
		t.Fatalf("summary = %+v", summary)
	}

	o := ObjectOutput[r80integEmailSummary](ObjectOutputOptions{Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{}))})
	if _, err := o.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{}`}); err == nil {
		t.Fatal("missing required priority: want an error")
	} else {
		var noObj *NoObjectGeneratedError
		if !errors.As(err, &noObj) {
			t.Fatalf("err = %T %v, want *NoObjectGeneratedError", err, err)
		}
	}
}

// GenerateObject with a struct schema validates the decoded object.
func TestR80integGenerateObjectStructSchema(t *testing.T) {
	res, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  r80integTextModel(`{"priority":"low"}`),
		Prompt: "x",
		Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := res.Object.(map[string]interface{}); !ok || m["priority"] != "low" {
		t.Fatalf("object = %#v", res.Object)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  r80integTextModel(`{"subject":"s"}`),
		Prompt: "x",
		Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
	}); err == nil {
		t.Fatal("missing required priority: want an error")
	}
}

// r80integApprovalMessages decodes a UI conversation whose last assistant
// message holds an approved tool part with the given approval JSON, and
// converts it to model messages.
func r80integApprovalMessages(t *testing.T, approval string) []types.Message {
	t.Helper()
	var msgs []UIMessage
	raw := `[{"id":"u1","role":"user","parts":[{"type":"text","text":"go"}]},` +
		`{"id":"a1","role":"assistant","parts":[{"type":"tool-tool1","toolCallId":"call-1","state":"approval-responded","input":{"value":"v"},"approval":` + approval + `}]}]`
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatal(err)
	}
	model, err := ConvertToModelMessages(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// stream CR 2 r80: a UI approval's "inputSchemaInput": null reaches the model
// messages and tool approval validation validates null (TS hasOwnProperty in
// convert-to-model-messages.ts and validate-tool-approvals.ts).
func TestR80integNullApprovalInputSchemaInputValidated(t *testing.T) {
	tool := types.Tool{
		Name: "tool1",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"value"},
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	for _, tc := range []struct {
		name     string
		approval string
		invalid  bool
		wantJSON string
	}{
		{"null", `{"id":"ap-1","approved":true,"inputSchemaInput":null}`, true, `"inputSchemaInput":null`},
		{"absent", `{"id":"ap-1","approved":true}`, false, ""},
		{"object", `{"id":"ap-1","approved":true,"inputSchemaInput":{"value":"v"}}`, false, `"inputSchemaInput":{"value":"v"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := r80integApprovalMessages(t, tc.approval)
			var request *types.ToolApprovalRequestContent
			for _, m := range msgs {
				for _, c := range m.Content {
					if r, ok := c.(types.ToolApprovalRequestContent); ok {
						request = &r
					}
				}
			}
			if request == nil {
				t.Fatalf("no approval request in %#v", msgs)
			}
			b, err := json.Marshal(*request)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantJSON == "" && strings.Contains(string(b), "inputSchemaInput") || tc.wantJSON != "" && !strings.Contains(string(b), tc.wantJSON) {
				t.Fatalf("request JSON = %s, want %q", b, tc.wantJSON)
			}
			collected, err := CollectToolApprovals(msgs)
			if err != nil || len(collected.ApprovedToolApprovals) != 1 {
				t.Fatalf("collected %+v, %v", collected, err)
			}
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: collected.ApprovedToolApprovals,
				Tools:                 []types.Tool{tool},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(res.InvalidToolApprovals) == 1; got != tc.invalid {
				t.Fatalf("invalid = %v (%+v), want %v", got, res, tc.invalid)
			}
		})
	}
}

// r80integAbortRun streams through StreamText with a model that blocks until
// its ctx ends, reads the UI chunks from ToUIMessageStream(uiCtx), cancels the
// StreamText ctx after the first text delta, and returns the chunk types and
// the UI error.
func r80integAbortRun(t *testing.T, shared bool) ([]string, error) {
	t.Helper()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		return &r79integBlockingStream{ctx: ctx, cancelled: make(chan struct{})}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	uiCtx := context.Background()
	if shared {
		uiCtx = ctx
	}
	chunks, errCh := r.ToUIMessageStream(uiCtx)
	var types []string
	for c := range chunks {
		typ, _ := c["type"].(string)
		types = append(types, typ)
		if typ == "text-delta" {
			cancel()
		}
	}
	return types, <-errCh
}

// One ctx drives both StreamText and the UI helper: once it is cancelled the
// UI conversion writes nothing more (a cancelled TS reader receives no
// further value, as the createUIMessageStream merge already does), so the
// abort chunk StreamText produces after the cancellation is never written.
// Before, Go's select picked the abort send or the ctx at random.
func TestR80integSharedCtxAbortIsDeterministic(t *testing.T) {
	types, err := r80integAbortRun(t, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("UI error = %v, want context.Canceled", err)
	}
	for _, typ := range types {
		if typ == "abort" {
			t.Fatalf("chunks = %v: the abort chunk was written after the UI ctx was cancelled", types)
		}
	}
}

// A separate UI ctx (TS: the UI helpers take no abort signal) always gets
// the abort chunk.
func TestR80integSeparateCtxAbortChunk(t *testing.T) {
	types, err := r80integAbortRun(t, false)
	if err != nil {
		t.Fatalf("UI error = %v", err)
	}
	found := false
	for _, typ := range types {
		found = found || typ == "abort"
	}
	if !found {
		t.Fatalf("chunks = %v, want an abort chunk", types)
	}
}
