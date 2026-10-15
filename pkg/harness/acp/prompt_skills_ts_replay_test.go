package acp

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func userMessage(parts ...types.ContentPart) harness.Prompt {
	return harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: parts}}
}

// Replays TS acp-v1-prompt.test.ts.
func TestTSReplay_ConvertPromptToTextBlocks(t *testing.T) {
	maps := []struct {
		name     string
		prompt   harness.Prompt
		expected []TextContentBlock
	}{
		{"string prompt", harness.TextPrompt("Hello"), []TextContentBlock{{Type: "text", Text: "Hello"}}},
		{"separate user message text parts", userMessage(types.TextContent{Text: "First"}, types.TextContent{Text: "Second"}),
			[]TextContentBlock{{Type: "text", Text: "First"}, {Type: "text", Text: "Second"}}},
		{"empty user message", userMessage(), []TextContentBlock{}},
	}
	for _, tc := range maps {
		t.Run("maps a "+tc.name, func(t *testing.T) {
			got, err := convertPromptToTextBlocks(tc.prompt, "portable-acp")
			if err != nil || !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("got %+v, %v", got, err)
			}
			if b, _ := json.Marshal(got); strings.Contains(string(b), "null") {
				t.Fatalf("encoded as %s", b)
			}
		})
	}
	rejects := []struct {
		name     string
		part     types.ContentPart
		category string
	}{
		{"legacy image part", types.ImageContent{}, "image content"},
		{"image file", types.FileContent{MediaType: "image/png"}, "image content"},
		{"audio file", types.FileContent{MediaType: "audio/mpeg"}, "audio content"},
		{"embedded resource file", types.FileContent{MediaType: "text/plain"}, `embedded resource content with media type "text/plain"`},
	}
	for _, tc := range rejects {
		t.Run("rejects "+tc.name+" precisely", func(t *testing.T) {
			_, err := convertPromptToTextBlocks(userMessage(tc.part), "portable-acp")
			var unsupported *harness.CapabilityUnsupportedError
			if !errors.As(err, &unsupported) || unsupported.HarnessID != "portable-acp" || !strings.Contains(err.Error(), tc.category) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTSReplay_PrependInstructionGuidance(t *testing.T) {
	got := prependInstructionGuidance([]TextContentBlock{{Type: "text", Text: "First"}, {Type: "text", Text: "Second"}}, "Prefer concise answers.")
	want := []TextContentBlock{
		{Type: "text", Text: "<session-guidance>\nThis block is operating guidance from the harness, not user-authored content.\n<instructions>\nPrefer concise answers.\n</instructions>\n</session-guidance>"},
		{Type: "text", Text: "First"}, {Type: "text", Text: "Second"},
	}
	if !reflect.DeepEqual(got, want) || strings.Contains(got[0].Text, "available-skills") {
		t.Fatalf("got %+v", got)
	}
	if got := prependInstructionGuidance([]TextContentBlock{{Type: "text", Text: "Hello"}}, ""); !reflect.DeepEqual(got, []TextContentBlock{{Type: "text", Text: "Hello"}}) {
		t.Fatalf("empty guidance block added: %+v", got)
	}
	if got := prependInstructionGuidance(nil, ""); got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty non-nil slice", got)
	}
}

// Replays TS acp-v1-skills.test.ts.
func TestTSReplay_Skills(t *testing.T) {
	skill := harness.Skill{Name: "release-notes", Description: "Prepare release notes.", Content: "Complete private skill content.",
		Files: []harness.SkillFile{{Path: "references/style.md", Content: "Use active voice."}}}
	t.Run("private session directory", func(t *testing.T) {
		const state = "/home/agent/.ai-sdk-harness"
		if got := resolvePrivateSessionDirectory(state, "session-1"); got != state+"/.agent-runs/session-1" {
			t.Fatal(got)
		}
		if got := resolvePrivateSessionDirectory(state, "../../../../work/project"); got != state+"/.agent-runs/..%2F..%2F..%2F..%2Fwork%2Fproject" {
			t.Fatal(got)
		}
	})
	t.Run("valid skills", func(t *testing.T) {
		if err := validateSkills([]harness.Skill{skill}); err != nil {
			t.Fatal(err)
		}
	})
	files := []struct {
		name  string
		files []harness.SkillFile
		err   string
	}{
		{"absolute path", []harness.SkillFile{{Path: "/escape.md"}}, "relative POSIX path"},
		{"Windows absolute path", []harness.SkillFile{{Path: `C:\\escape.md`}}, "relative POSIX path"},
		{"parent path", []harness.SkillFile{{Path: "../escape.md"}}, "relative POSIX path"},
		{"normalized-away traversal", []harness.SkillFile{{Path: "references/../escape.md"}}, "relative POSIX path"},
		{"reserved skill definition", []harness.SkillFile{{Path: "./SKILL.md"}}, "SKILL.md is reserved"},
		{"duplicate normalized path", []harness.SkillFile{{Path: "references/./style.md"}, {Path: "references/style.md"}}, "Duplicate ACP skill file path"},
	}
	for _, tc := range files {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			s := skill
			s.Files = tc.files
			if err := validateSkills([]harness.Skill{s}); err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
		})
	}
	t.Run("rejects invalid and duplicate skill names", func(t *testing.T) {
		bad := skill
		bad.Name = "../release-notes"
		if err := validateSkills([]harness.Skill{bad}); err == nil || !strings.Contains(err.Error(), "Invalid ACP skill name") {
			t.Fatal(err)
		}
		if err := validateSkills([]harness.Skill{skill, skill}); err == nil || !strings.Contains(err.Error(), "Duplicate ACP skill name") {
			t.Fatal(err)
		}
	})
	t.Run("skills directory", func(t *testing.T) {
		if got, err := resolveSkillsDirectory("/home/agent", ""); err != nil || got != "/home/agent/"+DefaultSkillsDirectory {
			t.Fatalf("%q %v", got, err)
		}
		if got, err := resolveSkillsDirectory("/home/agent", ".claude/skills"); err != nil || got != "/home/agent/.claude/skills" {
			t.Fatalf("%q %v", got, err)
		}
		for _, dir := range []string{".", "/skills", `C:\\skills`, `.agents\\skills`, "../skills"} {
			if _, err := resolveSkillsDirectory("/home/agent", dir); err == nil || !strings.Contains(err.Error(), "must be a relative POSIX path without traversal") {
				t.Errorf("%q accepted: %v", dir, err)
			}
		}
	})
}

// Replays TS acp-v1-lifecycle.test.ts `validateACPLifecycleCompatibility`
// ("rejects a mismatched $name before attach").
func TestTSReplay_LifecycleCompatibilityRejections(t *testing.T) {
	sandbox := replaySandbox(t, "tok")
	mk := func(id string) harness.Harness {
		h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" }; s.HarnessID = id }))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := mk("portable-acp")
	sess, err := h.DoStart(t.Context(), harness.StartOptions{SessionID: "s", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := sess.DoDetach(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	_ = json.Unmarshal(state.Data, &base)
	clone := func(mut func(map[string]any)) *harness.ResumeSessionState {
		var d map[string]any
		_ = json.Unmarshal(state.Data, &d)
		mut(d)
		out, err := harness.NewResumeSessionState(state.HarnessID, d)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	cases := []struct {
		name  string
		state *harness.ResumeSessionState
		h     harness.Harness
		err   string
	}{
		{"harness", func() *harness.ResumeSessionState { s := *state; s.HarnessID = "other-acp"; return &s }(), h, "produced by harness"},
		{"implementation", clone(func(d map[string]any) { d["implementationIdentity"] = "other-implementation" }), h, "configured implementation"},
		{"authentication profile", clone(func(d map[string]any) { d["authenticationProfile"].(map[string]any)["digest"] = "other-authentication" }), h, "authentication profile"},
		{"sandbox", clone(func(d map[string]any) { d["bridge"].(map[string]any)["sandboxId"] = "sandbox-2" }), h, "belongs to sandbox"},
	}
	for _, tc := range cases {
		t.Run("rejects a mismatched "+tc.name+" before attach", func(t *testing.T) {
			spawns := len(sandbox.spawnCommands)
			_, err := tc.h.DoStart(t.Context(), harness.StartOptions{SessionID: "s", SandboxSession: sandbox, SessionWorkDir: "/w", ResumeFrom: tc.state})
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
			if len(sandbox.spawnCommands) != spawns {
				t.Fatal("a rejected state must not spawn a bridge")
			}
		})
	}
}
