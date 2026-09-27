package opencode

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port of harness-opencode/src/opencode-auth.test.ts
// `createOpenCodeRequestTransformations` coverage, extended to the
// provider branches TS does not unit-test directly (xAI, GitHub Copilot,
// Poe, OpenCode Go, GitLab) but that createOpenCodeRequestTransformations
// (auth.go) implements via the same createBearerTransformation helper as
// the TS-tested openai/ai-gateway/anthropic branches.

func assertBearerTransformation(t *testing.T, got []harness.RequestTransformation, wantHost, wantMatchBearer, wantTransformBearer string) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("len(transformations) = %d, want 1: %+v", len(got), got)
	}
	tr := got[0]
	if tr.Match.Host != wantHost {
		t.Errorf("Match.Host = %q, want %q", tr.Match.Host, wantHost)
	}
	if len(tr.Match.Headers) != 1 {
		t.Fatalf("Match.Headers = %+v, want exactly 1 entry", tr.Match.Headers)
	}
	h := tr.Match.Headers[0]
	if h.Key == nil || h.Key.Exact != "Authorization" {
		t.Errorf("Match.Headers[0].Key = %+v, want exact Authorization", h.Key)
	}
	if h.Value == nil || h.Value.Exact != "Bearer "+wantMatchBearer {
		t.Errorf("Match.Headers[0].Value = %+v, want exact %q", h.Value, "Bearer "+wantMatchBearer)
	}
	if tr.Transform.Headers["Authorization"] != "Bearer "+wantTransformBearer {
		t.Errorf("Transform.Headers[Authorization] = %q, want %q", tr.Transform.Headers["Authorization"], "Bearer "+wantTransformBearer)
	}
}

func TestCreateOpenCodeRequestTransformations_XAI(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env:        map[string]string{"XAI_API_KEY": "xai-secret"},
			SandboxEnv: map[string]string{"XAI_API_KEY": "sandbox-xai-secret"},
			Auth:       AuthXAI,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "api.x.ai", "sandbox-xai-secret", "xai-secret")
	})

	t.Run("custom base URL", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env:        map[string]string{"XAI_API_KEY": "xai-secret", "XAI_BASE_URL": "https://xai.example/v1"},
			SandboxEnv: map[string]string{"XAI_API_KEY": "sandbox-xai-secret"},
			Auth:       AuthXAI,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "xai.example", "sandbox-xai-secret", "xai-secret")
	})

	t.Run("no credential yields no transformation", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env: map[string]string{}, SandboxEnv: map[string]string{}, Auth: AuthXAI,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("transformations = %+v, want none without a credential", got)
		}
	})
}

func TestCreateOpenCodeRequestTransformations_GitHubCopilot(t *testing.T) {
	t.Run("prefers GITHUB_COPILOT_TOKEN over GITHUB_TOKEN", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env: map[string]string{
				"GITHUB_COPILOT_TOKEN": "copilot-secret",
				"GITHUB_TOKEN":         "github-secret",
			},
			SandboxEnv: map[string]string{"GITHUB_COPILOT_TOKEN": "sandbox-copilot-secret"},
			Auth:       AuthGitHubCopilot,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "api.githubcopilot.com", "sandbox-copilot-secret", "copilot-secret")
	})

	t.Run("falls back to GITHUB_TOKEN", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env:        map[string]string{"GITHUB_TOKEN": "github-secret"},
			SandboxEnv: map[string]string{"GITHUB_TOKEN": "sandbox-github-secret"},
			Auth:       AuthGitHubCopilot,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "api.githubcopilot.com", "sandbox-github-secret", "github-secret")
	})
}

func TestCreateOpenCodeRequestTransformations_Poe(t *testing.T) {
	got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env:        map[string]string{"POE_API_KEY": "poe-secret"},
		SandboxEnv: map[string]string{"POE_API_KEY": "sandbox-poe-secret"},
		Auth:       AuthPoe,
	})
	if err != nil {
		t.Fatalf("createOpenCodeRequestTransformations: %v", err)
	}
	assertBearerTransformation(t, got, "api.poe.com", "sandbox-poe-secret", "poe-secret")
}

func TestCreateOpenCodeRequestTransformations_OpenCodeGo(t *testing.T) {
	got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env:        map[string]string{"OPENCODE_API_KEY": "oc-secret"},
		SandboxEnv: map[string]string{"OPENCODE_API_KEY": "sandbox-oc-secret"},
		Auth:       AuthOpenCodeGo,
	})
	if err != nil {
		t.Fatalf("createOpenCodeRequestTransformations: %v", err)
	}
	assertBearerTransformation(t, got, "opencode.ai", "sandbox-oc-secret", "oc-secret")
}

func TestCreateOpenCodeRequestTransformations_GitLab(t *testing.T) {
	t.Run("default instance", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env:        map[string]string{"GITLAB_TOKEN": "gitlab-secret"},
			SandboxEnv: map[string]string{"GITLAB_TOKEN": "sandbox-gitlab-secret"},
			Auth:       AuthGitLab,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "gitlab.com", "sandbox-gitlab-secret", "gitlab-secret")
	})

	t.Run("self-managed instance", func(t *testing.T) {
		got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
			Env:        map[string]string{"GITLAB_TOKEN": "gitlab-secret", "GITLAB_INSTANCE_URL": "https://gitlab.example.com"},
			SandboxEnv: map[string]string{"GITLAB_TOKEN": "sandbox-gitlab-secret"},
			Auth:       AuthGitLab,
		})
		if err != nil {
			t.Fatalf("createOpenCodeRequestTransformations: %v", err)
		}
		assertBearerTransformation(t, got, "gitlab.example.com", "sandbox-gitlab-secret", "gitlab-secret")
	})
}

// TS: "uses the resolved OpenAI route"
func TestCreateOpenCodeRequestTransformations_OpenAI(t *testing.T) {
	got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env: map[string]string{
			"OPENAI_API_KEY":      "openai-secret",
			"OPENAI_BASE_URL":     "https://openai.example/v1",
			"AI_GATEWAY_API_KEY":  "unselected-gateway-secret",
			"AI_GATEWAY_BASE_URL": "https://unselected-gateway.example/v1",
		},
		SandboxEnv: map[string]string{"OPENAI_API_KEY": "sandbox-openai-secret"},
		Auth:       AuthOpenAI,
	})
	if err != nil {
		t.Fatalf("createOpenCodeRequestTransformations: %v", err)
	}
	assertBearerTransformation(t, got, "openai.example", "sandbox-openai-secret", "openai-secret")
}

// TS: "injects both supported Anthropic credential headers"
func TestCreateOpenCodeRequestTransformations_Anthropic(t *testing.T) {
	got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env: map[string]string{
			"ANTHROPIC_API_KEY":    "api-secret",
			"ANTHROPIC_AUTH_TOKEN": "token-secret",
		},
		SandboxEnv: map[string]string{
			"ANTHROPIC_API_KEY":    "sandbox-api-secret",
			"ANTHROPIC_AUTH_TOKEN": "sandbox-token-secret",
		},
		Auth: AuthAnthropic,
	})
	if err != nil {
		t.Fatalf("createOpenCodeRequestTransformations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(transformations) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Match.Host != "api.anthropic.com" || got[0].Match.Headers[0].Key.Exact != "x-api-key" ||
		got[0].Match.Headers[0].Value.Exact != "sandbox-api-secret" || got[0].Transform.Headers["x-api-key"] != "api-secret" {
		t.Errorf("transformations[0] = %+v", got[0])
	}
	if got[1].Match.Host != "api.anthropic.com" || got[1].Match.Headers[0].Key.Exact != "Authorization" ||
		got[1].Match.Headers[0].Value.Exact != "Bearer sandbox-token-secret" || got[1].Transform.Headers["Authorization"] != "Bearer token-secret" {
		t.Errorf("transformations[1] = %+v", got[1])
	}
}
