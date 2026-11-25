package vercel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createVercelNetworkSandboxSession / resumeVercelNetworkSandboxSession
// call effectiveSignal?.throwIfAborted() before anything else, so an
// aborted call rejects with the abort reason even when the options are
// otherwise invalid or no credentials are configured.
func TestR93toolsVercelAbortedBeforeAnything(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN_FILE", "")
	reason := errors.New("stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	if _, err := CreateNetworkSandboxSession(ctx, CreateSessionOptions{SandboxID: "a", Name: "b"}); !errors.Is(err, reason) {
		t.Fatalf("create err = %v, want the abort reason", err)
	}
	if _, err := ResumeNetworkSandboxSession(ctx, ResumeSessionOptions{SandboxID: "a"}); !errors.Is(err, reason) {
		t.Fatalf("resume err = %v, want the abort reason", err)
	}
}

// TS isVercelSandboxAuthenticationFailure tests the message of each error in
// the cause chain against VERCEL_AUTHENTICATION_ERROR_MESSAGE (/i, no u
// flag): a template preparation failure whose message says "Authentication
// failed" is a HarnessSandboxAuthenticationError.
func TestR93toolsVercelAuthMessageInCauseChain(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	prepErr := fmt.Errorf("git clone: %w", errors.New("fatal: Authentication failed for 'https://example.com/repo.git'"))
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(),
		Template: &Template{Identity: "auth", Prepare: func(context.Context, providerutils.SandboxSession) error { return prepErr }},
	})
	var authErr *harness.SandboxAuthenticationError
	if !errors.As(err, &authErr) || !errors.Is(err, prepErr) {
		t.Fatalf("err = %T %v, want a SandboxAuthenticationError caused by the prepare error", err, err)
	}

	for _, tc := range []struct {
		msg  string
		auth bool
	}{
		{"no authentication found", true},
		{"Failed to refresh authentication token", true},
		{"Missing credentials parameters to access the Vercel API", true},
		{"Could not get credentials from OIDC context", true},
		{"Authentication succeeded", false},
		{"Miſſing credentials parameters to access the Vercel API", false},
		{"Failed to refresh authentication toKen", false},
	} {
		got := withAuthError(errors.New(tc.msg))
		if isAuth := errors.As(got, &authErr); isAuth != tc.auth {
			t.Errorf("%q: auth = %v, want %v", tc.msg, isAuth, tc.auth)
		}
	}
}
