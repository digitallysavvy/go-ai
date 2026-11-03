package harness

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// r85integFileOpsSandbox adds the optional file operations tools added in
// round 85 (TS stat / listDirectory / deleteFile) to the mock sandbox.
type r85integFileOpsSandbox struct {
	*mockSandbox
	calls atomic.Int32
}

func (s *r85integFileOpsSandbox) Stat(context.Context, string) (*providerutils.SandboxFileStat, error) {
	s.calls.Add(1)
	return &providerutils.SandboxFileStat{Type: providerutils.SandboxPathTypeFile, Size: 3}, nil
}

func (s *r85integFileOpsSandbox) ListDirectory(context.Context, string) ([]providerutils.SandboxDirectoryEntry, error) {
	s.calls.Add(1)
	return []providerutils.SandboxDirectoryEntry{{Name: "a", Type: providerutils.SandboxPathTypeFile}}, nil
}

func (s *r85integFileOpsSandbox) DeleteFile(context.Context, string) (bool, error) {
	s.calls.Add(1)
	return true, nil
}

// TS delegateSandboxOperations (8a20b57d41) forwards stat, listDirectory and
// deleteFile through the deferred session after the setup ran; Go reports
// errors.ErrUnsupported when the original sandbox lacks the method (carry
// r85 CR to tools / integrator).
func TestR85integDeferredSandboxForwardsFileOperations(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})
	a, sessions := r85carrySetupAgent(t, mock.harness, nil)
	sb := &r85integFileOpsSandbox{mockSandbox: newMockSandbox()}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: sb})
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []providerutils.SandboxSession{session.GetSandboxSession(), GetRestrictedSandboxSession(session.GetSandboxSession())} {
		st, ok := view.(providerutils.SandboxStatter)
		if !ok {
			t.Fatalf("%T has no Stat", view)
		}
		if got, err := st.Stat(context.Background(), "f"); err != nil || got.Size != 3 {
			t.Fatalf("Stat = %+v, %v", got, err)
		}
		if got, err := view.(providerutils.SandboxDirectoryLister).ListDirectory(context.Background(), "d"); err != nil || len(got) != 1 {
			t.Fatalf("ListDirectory = %+v, %v", got, err)
		}
		if ok, err := view.(providerutils.SandboxFileDeleter).DeleteFile(context.Background(), "f"); err != nil || !ok {
			t.Fatalf("DeleteFile = %v, %v", ok, err)
		}
	}
	if n := atomic.LoadInt32(sessions); n != 1 || sb.calls.Load() != 6 {
		t.Fatalf("onSession=%d calls=%d, want 1 setup and 6 forwarded calls", n, sb.calls.Load())
	}

	plain, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: newMockSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.GetSandboxSession().(providerutils.SandboxStatter).Stat(context.Background(), "f"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Stat on a sandbox without it = %v, want ErrUnsupported", err)
	}
}
