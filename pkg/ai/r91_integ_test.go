package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r91integStreamFileModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/a.png"}}},
			{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &types.ReasoningFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/r.png"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}), nil
	}}
}

// TS 2ebc78b654 (stream-language-model-call.ts passes download to
// resolveGeneratedFileData; loop-r91-stream item 2): StreamText downloads
// generated and reasoning file URLs with ExperimentalDownload.
func TestR91integStreamGeneratedFileUsesExperimentalDownload(t *testing.T) {
	t.Run("custom download", func(t *testing.T) {
		var got []promptutils.DownloadRequest
		res, err := StreamText(context.Background(), StreamTextOptions{Model: r91integStreamFileModel(), Prompt: "hi",
			ExperimentalDownload: func(_ context.Context, reqs []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				got = append(got, reqs...)
				return []*promptutils.DownloadResult{{Data: []byte("png"), MediaType: "image/png"}}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].URL != "https://files.example.com/a.png" || got[1].URL != "https://files.example.com/r.png" || got[0].IsURLSupportedByModel || got[1].IsURLSupportedByModel {
			t.Fatalf("download requests = %+v", got)
		}
		if files := res.Files(); len(files) != 1 || string(files[0].Data) != "png" {
			t.Fatalf("files = %+v", files)
		}
	})
	t.Run("nil result", func(t *testing.T) {
		res, err := StreamText(context.Background(), StreamTextOptions{Model: r91integStreamFileModel(), Prompt: "hi",
			ExperimentalDownload: func(context.Context, []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				return []*promptutils.DownloadResult{nil}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = res.ReadAll()
		err = res.Err()
		var dlErr *providererrors.DownloadError
		if !errors.As(err, &dlErr) || err.Error() != "Generated file URL https://files.example.com/a.png must be downloaded, but the download function returned null" {
			t.Fatalf("err = %v", err)
		}
	})
}

// core1-01 (TS advisor_20260301.ts: z.object({}).strict()): an advisor call
// with an input key is an invalid tool call, not stripped to {}; a stripping
// tool (bash_20250124) still drops undeclared keys.
func TestR91integAdvisorInputIsStrict(t *testing.T) {
	advisor := types.Tool{Name: "advisor", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.advisor_20260301",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}}
	inv, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "c1", ToolName: "advisor", RawArguments: `{"x":1}`},
		Tools:    []types.Tool{advisor},
	})
	if err != nil || !inv.Invalid || !IsInvalidToolInputError(inv.Error) {
		t.Fatalf("advisor = %+v, %v; want an invalid call with InvalidToolInputError", inv, err)
	}
	bash := types.Tool{Name: "bash", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}}}}
	got, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "c2", ToolName: "bash", RawArguments: `{"command":"ls","x":1}`},
		Tools:    []types.Tool{bash},
	})
	if err != nil || len(got.Arguments) != 1 || got.Arguments["command"] != "ls" {
		t.Fatalf("bash = %+v, %v", got.Arguments, err)
	}
}

// core1-02 (TS tool-caller-configuration.ts 32-55): an unset callers entry
// leaves the tool unaffected, and only a nil tool set short-circuits.
// OVC-01: a provider caller sees a typed Tool.ProviderOptions as its JSON
// object.
func TestR91integToolCallersUnsetEntryAndTypedOptions(t *testing.T) {
	tools := []types.Tool{{Name: "a"}, {Name: "b"}}
	resolved, err := ResolveToolCallerConfiguration(tools, ExperimentalToolCallers{"a": nil})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resolved["a"]; ok {
		t.Fatalf("resolved = %v, want no entry for an unset callers list", resolved)
	}
	_, model, _ := PrepareToolsForToolCallers(tools, resolved)
	if len(model) != 2 {
		t.Fatalf("model tools = %d, want both", len(model))
	}
	var argErr *providererrors.InvalidArgumentError
	if _, err := ResolveToolCallerConfiguration([]types.Tool{}, ExperimentalToolCallers{"x": {DirectToolCall}}); !errors.As(err, &argErr) || argErr.Message != `unknown tool "x".` {
		t.Fatalf("empty tool set: err = %v", err)
	}

	var seen map[string]interface{}
	caller := types.Tool{Name: "pc", ExperimentalToolCaller: &types.ToolCallerDefinition{Type: types.ToolCallerTypeProvider,
		PrepareProviderOptions: func(in map[string]interface{}) map[string]interface{} { seen = in; return in }}}
	callee := types.Tool{Name: "callee", ProviderOptions: map[string]map[string]interface{}{"anthropic": {"cacheControl": map[string]interface{}{"type": "ephemeral"}}}}
	r, err := ResolveToolCallerConfiguration([]types.Tool{caller, callee}, ExperimentalToolCallers{"callee": {"pc"}})
	if err != nil {
		t.Fatal(err)
	}
	PrepareToolsForToolCallers([]types.Tool{caller, callee}, r)
	anth, _ := seen["anthropic"].(map[string]interface{})
	if cc, _ := anth["cacheControl"].(map[string]interface{}); cc["type"] != "ephemeral" {
		t.Fatalf("PrepareProviderOptions got %#v", seen)
	}
}
