package harness

import (
	"context"
	"strings"
	"testing"
)

// TS createSession uses the generated session id for the sandbox and its
// work dir (`<harnessId>-<sessionId>`). Without it, every session created
// without an explicit SessionID shared one `<harnessId>-%` directory.
func TestCreateSession_GeneratedSessionIDNamesWorkDir(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{})
	a, err := NewAgent(AgentSettings{Harness: mock.harness})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	first, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	second, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, s := range []*AgentSession{first, second} {
		want := EncodePathSegment(mock.harness.HarnessID()) + "-" + EncodePathSegment(s.SessionID())
		if !strings.HasSuffix(s.GetSessionWorkDir(), "/"+want) {
			t.Fatalf("work dir %q does not end with %q", s.GetSessionWorkDir(), want)
		}
	}
	if first.GetSessionWorkDir() == second.GetSessionWorkDir() {
		t.Fatalf("two sessions share work dir %q", first.GetSessionWorkDir())
	}
}
