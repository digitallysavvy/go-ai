package opencode

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func r87toolsGitLabTransformation(host, path, placeholder string) []harness.RequestTransformation {
	return []harness.RequestTransformation{{
		Match: harness.RequestTransformationMatch{
			Host:   host,
			Path:   &harness.StringMatcher{Exact: path},
			Method: []string{"POST"},
			Headers: []harness.KeyValueMatcher{{
				Key:   &harness.StringMatcher{Exact: "Authorization"},
				Value: &harness.StringMatcher{Exact: "Bearer " + placeholder},
			}},
		},
		Transform: harness.RequestTransformationTransform{Headers: map[string]string{"Authorization": "Bearer gitlab-secret"}},
	}}
}

// TS opencode-auth.test.ts "restricts GitLab credentials to the POST chat
// exchange for $instanceUrl" (138d66eb78): exact path, POST only, the exact
// placeholder header; string concatenation keeps a trailing slash's `//`.
func TestR87toolsGitLabTransformationIsChatExchangeOnly(t *testing.T) {
	cases := []struct {
		instanceURL *string
		host, path  string
	}{
		{nil, "gitlab.com", "/api/v4/ai/third_party_agents/direct_access"},
		{r87toolsPtr("https://gitlab.example"), "gitlab.example", "/api/v4/ai/third_party_agents/direct_access"},
		{r87toolsPtr("https://gitlab.example/gitlab"), "gitlab.example", "/gitlab/api/v4/ai/third_party_agents/direct_access"},
		{r87toolsPtr("https://gitlab.example/"), "gitlab.example", "//api/v4/ai/third_party_agents/direct_access"},
		{r87toolsPtr("https://gitlab.example/gitlab/"), "gitlab.example", "/gitlab//api/v4/ai/third_party_agents/direct_access"},
		// WHATWG `new URL` (not net/url): host lowercased, default port
		// dropped, dot segments resolved.
		{r87toolsPtr("https://GitLab.Example:443/a/../gitlab"), "gitlab.example", "/gitlab/api/v4/ai/third_party_agents/direct_access"},
	}
	for _, tc := range cases {
		env := map[string]string{"GITLAB_TOKEN": "gitlab-secret"}
		name := "default"
		if tc.instanceURL != nil {
			env["GITLAB_INSTANCE_URL"] = *tc.instanceURL
			name = *tc.instanceURL
		}
		t.Run(name, func(t *testing.T) {
			got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
				Env: env, SandboxEnv: map[string]string{"GITLAB_TOKEN": "sandbox-gitlab-placeholder"}, Auth: AuthGitLab,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := r87toolsGitLabTransformation(tc.host, tc.path, "sandbox-gitlab-placeholder")
			if !reflect.DeepEqual(got, want) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				t.Fatalf("transformations =\n%s\nwant\n%s", gb, wb)
			}
		})
	}
}

func r87toolsPtr(s string) *string { return &s }

// TS "omits GitLab transformations when a credential is missing".
func TestR87toolsGitLabTransformationNeedsBothCredentials(t *testing.T) {
	for _, in := range []createRequestTransformationsInput{
		{Env: map[string]string{}, SandboxEnv: map[string]string{"GITLAB_TOKEN": "sandbox-gitlab-placeholder"}},
		{Env: map[string]string{"GITLAB_TOKEN": "gitlab-secret"}, SandboxEnv: map[string]string{}},
	} {
		in.Auth = AuthGitLab
		got, err := createOpenCodeRequestTransformations(in)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %+v, %v; want none", got, err)
		}
	}
}

// An unparsable instance URL fails like TS `new URL` ("Invalid URL") and
// does not echo the URL.
func TestR87toolsGitLabTransformationInvalidInstanceURL(t *testing.T) {
	_, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env:        map[string]string{"GITLAB_TOKEN": "gitlab-secret", "GITLAB_INSTANCE_URL": "https://user:pw@[bad"},
		SandboxEnv: map[string]string{"GITLAB_TOKEN": "sandbox-gitlab-placeholder"}, Auth: AuthGitLab,
	})
	if err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("err = %v, want Invalid URL", err)
	}
}

// TS opencode-harness.test.ts "restricts direct GitLab credentials to the
// chat exchange on startup and resume".
func TestR87toolsGitLabBrokeringOnStartupAndResume(t *testing.T) {
	const token = "r87-gitlab-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(string) string { return token },
		Provider:        "gitlab",
		Auth: harness.AuthEnvironment(map[string]string{
			"GITLAB_TOKEN": "gitlab-secret", "GITLAB_INSTANCE_URL": "https://gitlab.example",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	sandbox.mu.Lock()
	placeholder := sandbox.spawnEnvs[0]["GITLAB_TOKEN"]
	blob, _ := json.Marshal(sandbox.spawnEnvs)
	sandbox.mu.Unlock()
	if !regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`).MatchString(placeholder) {
		t.Fatalf("placeholder = %q", placeholder)
	}
	if strings.Contains(string(blob), "gitlab-secret") {
		t.Fatalf("spawn env leaked the real credential: %s", blob)
	}
	want := r87toolsGitLabTransformation("gitlab.example", "/api/v4/ai/third_party_agents/direct_access", placeholder)
	sandbox.addReqTransMu.Lock()
	if len(sandbox.addReqTrans) != 1 || !reflect.DeepEqual(sandbox.addReqTrans[0], want) {
		t.Fatalf("addRequestTransformations = %+v, want one call with %+v", sandbox.addReqTrans, want)
	}
	sandbox.addReqTransMu.Unlock()

	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	var data resumeStateData
	if err := json.Unmarshal(resumeFrom.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data.SandboxCredentialEnvironment, map[string]string{"GITLAB_TOKEN": placeholder}) {
		t.Fatalf("sandboxCredentialEnvironment = %v", data.SandboxCredentialEnvironment)
	}
	resumed, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox, ResumeFrom: resumeFrom,
	})
	if err != nil {
		t.Fatalf("DoStart (resume): %v", err)
	}
	t.Cleanup(func() { _ = resumed.DoDestroy(context.Background()) })
	sandbox.addReqTransMu.Lock()
	defer sandbox.addReqTransMu.Unlock()
	if len(sandbox.addReqTrans) != 2 || !reflect.DeepEqual(sandbox.addReqTrans[1], want) {
		t.Fatalf("addRequestTransformations after resume = %+v", sandbox.addReqTrans)
	}
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnEnvs) != 1 {
		t.Fatalf("spawns = %d, want 1", len(sandbox.spawnEnvs))
	}
}
