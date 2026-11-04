package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// V061 item 23, StreamText / UI side (cr-r86b-gen-to-stream 2, applied by the
// integrator): TS convert-to-model-messages copies approval.reason into the
// approval response and the execution-denied output, so a decoded
// `"reason": ""` stays "" (TS `reason ?? default`).
func TestR86bintegUIEmptyApprovalReasonKept(t *testing.T) {
	var msgs []UIMessage
	raw := `[{"id":"m1","role":"user","parts":[{"type":"text","text":"hi"}]},` +
		`{"id":"m2","role":"assistant","parts":[{"type":"tool-tool1","toolCallId":"call-1","state":"approval-responded","input":{},` +
		`"approval":{"id":"a1","approved":false,"reason":""}}]}]`
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatal(err)
	}
	out, err := ConvertToModelMessages(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	var sawResponse, sawDenied bool
	for _, m := range out {
		for _, p := range m.Content {
			switch v := p.(type) {
			case types.ToolApprovalResponseContent:
				sawResponse = true
				if !v.HasReason() {
					t.Errorf("approval response lost the empty reason: %#v", v)
				}
			case types.ToolResultContent:
				if v.Output != nil && v.Output.Type == types.ToolResultOutputExecutionDenied {
					sawDenied = true
					b, _ := json.Marshal(v.Output)
					if string(b) != `{"type":"execution-denied","reason":""}` {
						t.Errorf("execution-denied output = %s, want the empty reason kept", b)
					}
				}
			}
		}
	}
	if !sawResponse || !sawDenied {
		t.Fatalf("missing parts: response %v denied %v in %#v", sawResponse, sawDenied, out)
	}
}

// StreamText policy denial with Reason &"" (TS needsApproval / toolApproval
// returning a denial with an empty reason): the next prompt's tool message is
// {type: "execution-denied", reason: ""}, as GenerateText (gen 0dee3cf2).
func TestR86bintegStreamPolicyEmptyDenialReasonKept(t *testing.T) {
	var prompts [][]types.Message
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		prompts = append(prompts, o.Prompt.Messages)
		calls++
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c2", ToolName: "tool1", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "1", Text: "ok"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
		}), nil
	}}
	empty := ""
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "x", nil
		}}},
		ToolApproval: types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &empty}
		}),
		Prompt:   "hi",
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("model calls = %d", len(prompts))
	}
	last := prompts[1][len(prompts[1])-1]
	b, _ := json.Marshal(last)
	if !strings.Contains(string(b), `"output":{"type":"execution-denied","reason":""}`) {
		t.Fatalf("next prompt tool message = %s, want the empty reason kept", b)
	}
}

// V061 item 12 (cr-r86b-gen-to-integrator 1): ShellSandbox implements the
// optional SandboxSession operations TS just-bash sessions have (stat,
// listDirectory, deleteFile), with the local harness sandbox's contracts.
func TestR86bintegShellSandboxOptionalOperations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	sb := NewShellSandbox()

	if st, err := sb.Stat(ctx, file); err != nil || st == nil || st.Type != providerutils.SandboxPathTypeFile || st.Size != 3 {
		t.Errorf("Stat(file) = %+v, %v", st, err)
	}
	if st, err := sb.Stat(ctx, filepath.Join(dir, "link")); err != nil || st == nil || st.Type != providerutils.SandboxPathTypeFile {
		t.Errorf("Stat(link) = %+v, %v; want the target (followed)", st, err)
	}
	if st, err := sb.Stat(ctx, filepath.Join(dir, "sub")); err != nil || st == nil || st.Type != providerutils.SandboxPathTypeDirectory {
		t.Errorf("Stat(dir) = %+v, %v", st, err)
	}
	for _, missing := range []string{filepath.Join(dir, "nope"), filepath.Join(file, "x")} {
		if st, err := sb.Stat(ctx, missing); st != nil || err != nil {
			t.Errorf("Stat(%s) = %+v, %v; want nil, nil", missing, st, err)
		}
	}

	entries, err := sb.ListDirectory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.Type
	}
	want := map[string]string{"a.txt": "file", "sub": "directory", "link": "symlink"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListDirectory = %v, want %v", got, want)
	}
	if e, err := sb.ListDirectory(ctx, filepath.Join(dir, "nope")); e != nil || err != nil {
		t.Errorf("ListDirectory(missing) = %v, %v", e, err)
	}
	if _, err := sb.ListDirectory(ctx, file); err == nil || err.Error() != "Failed to list "+file+": Not a directory" {
		t.Errorf("ListDirectory(file) err = %v", err)
	}

	if ok, err := sb.DeleteFile(ctx, filepath.Join(dir, "link")); !ok || err != nil {
		t.Errorf("DeleteFile(link) = %v, %v", ok, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("deleting the link removed its target: %v", err)
	}
	if ok, err := sb.DeleteFile(ctx, filepath.Join(dir, "nope")); ok || err != nil {
		t.Errorf("DeleteFile(missing) = %v, %v", ok, err)
	}
	sub := filepath.Join(dir, "sub")
	if _, err := sb.DeleteFile(ctx, sub); err == nil || err.Error() != "Cannot delete "+sub+": it is a directory." {
		t.Errorf("DeleteFile(dir) err = %v", err)
	}
	if ok, err := sb.DeleteFile(ctx, file); !ok || err != nil {
		t.Errorf("DeleteFile(file) = %v, %v", ok, err)
	}
}

// cr-r86b-tools-to-integrator 1 and the data-url class: TS getTextFromDataUrl
// (ai/src/util/data-url.ts) matches the charset with JS \s and /i without the
// u flag, returns atob's binary string when there is no charset (each byte
// one character), and decodes with new TextDecoder(charset) (WHATWG labels,
// not Node Buffer encodings). Wants printed by Node 24 running the TS
// function.
func TestR86bintegGetTextFromDataURLMatchesTS(t *testing.T) {
	for _, tc := range []struct{ in, want, err string }{
		{"data:text/plain;base64,w6k=", "\u00c3\u00a9", ""},
		{"data:text/plain;charset=utf-8;base64,77u/w6n/", "\u00e9\ufffd", ""},
		{"data:text/plain;charset=iso-8859-1;base64,Y2Fm6YA=", "caf\u00e9\u20ac", ""},
		{"data:text/plain;charset=windows-1252;base64,gA==", "\u20ac", ""},
		{"data:text/plain;charset=latin1;base64,6Q==", "\u00e9", ""},
		{"data:text/plain;charset=utf-16le;base64,//5BAADY", "A\ufffd", ""},
		{"data:text/plain;charset=utf-16be;base64,/v8AQQ==", "A", ""},
		{"data:text/plain;charset=base64;base64,eA==", "", "Error decoding data URL"},
		{"data:text/plain;charset=bogus;base64,eA==", "", "Error decoding data URL"},
		{"data:text/plain;charset=\"UTF-8\";base64,b2s=", "ok", ""},
		{"data:text/plain; charset=windows-1252,6Q==", "\u00e9", ""},
		{"data:text/plain;\u00a0charset=windows-1252;base64,6Q==", "\u00e9", ""},
		{"data:text/plain;charset=windows-1252\u00a0;base64,6Q==", "\u00e9", ""},
		{"data:text/plain;char\u017fet=windows-1252;base64,gA==", "\u0080", ""}, // no charset: TS /i does not fold s to U+017F
		{"data:text/plain;CHARSET=Shift_JIS;base64,gqA=", "\u3042", ""},
		{"data:text/plain;charset=iso-2022-kr;base64,YWI=", "", "Error decoding data URL"},
	} {
		got, err := GetTextFromDataURL(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("GetTextFromDataURL(%q) = %q, %v; want error %q", tc.in, got, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("GetTextFromDataURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// cr-r86b-stream-to-integrator 2: TS generate-speech.ts 314 / 330
// mediaType.split(';', 1)[0].trim().toLowerCase() and
// outputFormat.trim().toLowerCase().
func TestR86bintegSpeechMediaTypeJSTrim(t *testing.T) {
	if got := responseAudioMediaType(map[string]string{"Content-Type": "audio/mpeg\ufeff"}); got != "audio/mpeg" {
		t.Errorf("U+FEFF-padded media type = %q, want audio/mpeg", got)
	}
	if got := responseAudioMediaType(map[string]string{"content-type": "\u0085audio/mpeg"}); got != "" {
		t.Errorf("U+0085-prefixed media type = %q, want none (TS does not trim U+0085)", got)
	}
	if got := outputFormatMediaType("PCM\ufeff"); got != "audio/pcm" {
		t.Errorf("outputFormatMediaType = %q, want audio/pcm", got)
	}
}
