package acp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS (#23587): acpContentBlockSchema is a text|image discriminated union; text
// blocks always carry `text` and image blocks carry `data` and `mimeType`.
func TestContentBlockWireShape(t *testing.T) {
	for in, want := range map[string]ContentBlock{
		`{"type":"text","text":""}`:                             {Type: "text"},
		`{"type":"text","text":"hi"}`:                           {Type: "text", Text: "hi"},
		`{"type":"image","data":"AQID","mimeType":"image/png"}`: {Type: "image", Data: "AQID", MimeType: "image/png"},
	} {
		got, err := json.Marshal(want)
		if err != nil || string(got) != in {
			t.Fatalf("Marshal(%+v) = %s, %v; want %s", want, got, err, in)
		}
		var back ContentBlock
		if err := json.Unmarshal([]byte(in), &back); err != nil || back != want {
			t.Fatalf("Unmarshal(%s) = %+v, %v", in, back, err)
		}
	}
}

// A persisted turnStartConfig may now carry image blocks.
func TestValidateLifecycleStateDataAcceptsImagePromptBlocks(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	v := h.(*acpHarness)
	cfg := func(block string) string {
		return `{"agentIdentity":"i","turnStartConfig":{"version":1,"configurationFingerprint":"f","prompt":[` + block + `],"permissionMode":"allow-all"}}`
	}
	if err := v.ValidateLifecycleStateData([]byte(cfg(`{"type":"image","data":"AQID","mimeType":"image/png"}`))); err != nil {
		t.Fatalf("image block rejected: %v", err)
	}
	for _, bad := range []string{`{"type":"image","data":"AQID"}`, `{"type":"image","mimeType":"image/png"}`, `{"type":"audio","data":"x","mimeType":"audio/mpeg"}`} {
		if err := v.ValidateLifecycleStateData([]byte(cfg(bad))); err == nil {
			t.Errorf("block %s accepted", bad)
		}
	}
}

// TS (#23587): inline images reach the bridge as ACP image content blocks.
func TestDoPromptTurnSendsImageBlocks(t *testing.T) {
	const token = "image-token"
	got := make(chan map[string]any, 1)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		got <- start
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return token } }))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "image-session", SessionWorkDir: "/vercel/sandbox/test-acp-image", SandboxSession: newFakeSandbox(srv),
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: userMessage(types.TextContent{Text: "Describe this"}, types.FileContent{MediaType: "image/png", Data: []byte{1, 2, 3}}),
		Emit:   func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case start := <-got:
		want := []any{
			map[string]any{"type": "text", "text": "Describe this"},
			map[string]any{"type": "image", "data": "AQID", "mimeType": "image/png"},
		}
		if !reflect.DeepEqual(start["prompt"], want) {
			t.Fatalf("start.prompt = %#v, want %#v", start["prompt"], want)
		}
		tsc, _ := start["turnStartConfig"].(map[string]any)
		if !reflect.DeepEqual(tsc["prompt"], want) {
			t.Fatalf("turnStartConfig.prompt = %#v, want %#v", tsc["prompt"], want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}
	<-control.Done()
}
