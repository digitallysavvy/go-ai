package vercel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TS VercelSandboxSession.writeBinaryFile mkdir -p's posix.dirname(path):
// Node keeps ".." segments and drops trailing slashes before taking the
// parent, where Go's path.Dir cleans the result.
func TestR95toolsVercelWriteBinaryFileParentIsNodeDirname(t *testing.T) {
	for path, want := range map[string][]string{
		"new/../f.txt": {"-p new/.."},
		"/a/b/":        {"-p /a"},
		"a//b":         {"-p a/"},
		"./x/y":        {"-p ./x"},
		"/f":           nil,
		"f":            nil,
	} {
		f := newFakeServer()
		var mkdirCalls []string
		f.CommandFn = func(_, command string, args []string, _ string, _ map[string]string) CommandScript {
			if command == "mkdir" {
				mkdirCalls = append(mkdirCalls, strings.Join(args, " "))
			}
			return CommandScript{}
		}
		sbx := newTestSandbox(t, f, "sbx_r95_dirname")
		_ = NewSession(sbx).WriteBinaryFile(context.Background(), path, []byte("x"))
		f.Close()
		if strings.Join(mkdirCalls, "|") != strings.Join(want, "|") {
			t.Errorf("%q: mkdir calls %v, want %v", path, mkdirCalls, want)
		}
	}
}

// TS isFileNotFoundError tests /no such file|not found|does not exist|ENOENT/i:
// without the u flag, s does not match U+017F (Go (?i) folds it).
func TestR95toolsVercelFileNotFoundMessageNoLongSFold(t *testing.T) {
	for msg, want := range map[string]bool{
		"No Such File":      true,
		"no ſuch file":      false,
		"it doeſ not exist": false,
		"DOES NOT EXIST":    true,
		"enoent":            true,
	} {
		if got := isFileNotFoundError(errors.New(msg)); got != want {
			t.Errorf("isFileNotFoundError(%q) = %v, want %v", msg, got, want)
		}
	}
}

// TS createStdinStream errors the stream with abortSignal.reason, and the
// @vercel/sandbox stdin retry wait and an aborted fetch reject with
// signal.reason: the cancel cause, not context.Canceled.
func TestR95toolsVercelStdinAbortCause(t *testing.T) {
	cause := errors.New("r95 stop")

	// The stream: a write after the spawn context ended.
	ctx, cancel := context.WithCancelCause(context.Background())
	s := &commandStdin{ctx: ctx}
	cancel(cause)
	if _, err := s.Write([]byte("x")); !errors.Is(err, cause) {
		t.Fatalf("Write after abort = %v, want the cause", err)
	}
	if err := s.Close(); !errors.Is(err, cause) {
		t.Fatalf("Close after abort = %v, want the cause", err)
	}

	// The retry wait.
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	cancel2(cause)
	if err := waitBeforeStdinRetry(ctx2, time.Hour); !errors.Is(err, cause) {
		t.Fatalf("waitBeforeStdinRetry = %v, want the cause", err)
	}

	// An in-flight request aborted mid-flight.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	defer close(release)
	c := NewAPIClient(srv.URL, Credentials{Token: "t"})
	ctx3, cancel3 := context.WithCancelCause(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel3(cause)
	}()
	zero := int64(0)
	if _, err := c.writeCommandStdin(ctx3, "s", "c", writeStdinParams{data: []byte("x"), offset: &zero}); err != cause { //nolint:errorlint // TS rethrows the reason itself
		t.Fatalf("aborted writeCommandStdin = %v, want the cause", err)
	}
}
