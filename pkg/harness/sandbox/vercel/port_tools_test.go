package vercel

import (
	"context"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS 1d2c2ff2e4 (vercel-sandbox-session.test.ts "drains capped output to
// completion without using native output buffers").
func TestPortToolsSessionRunBoundedKeepsPrefixAndLengths(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var gotArgs []string
	f.CommandFn = func(_, _ string, args []string, _ string, _ map[string]string) CommandScript {
		gotArgs = args
		return CommandScript{Stdout: "ab" + strings.Repeat("x", 4<<20), Stderr: "error!", ExitCode: 7}
	}
	sbx := newTestSandbox(t, f, "sbx_bounded")
	result, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "noisy; exit 7", MaxOutputLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := providerutils.SandboxRunResult{Stdout: "abxx", Stderr: "erro", ExitCode: 7, StdoutLength: 2 + 4<<20, StderrLength: 6}
	if result != want {
		t.Fatalf("result = %+v", result)
	}
	if len(gotArgs) != 2 || gotArgs[1] != "noisy; exit 7" {
		t.Fatalf("args = %v", gotArgs)
	}
}

func TestPortToolsSessionRunBoundedEmptyOutputAndUnicode(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(_, _ string, _ []string, _ string, _ map[string]string) CommandScript {
		return CommandScript{Stdout: "a😀b"}
	}
	sbx := newTestSandbox(t, f, "sbx_bounded_unicode")
	result, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "unicode", MaxOutputLength: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "a" || result.Stderr != "" || result.StdoutLength != 4 || result.StderrLength != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPortToolsSessionRunBoundedRejectsInvalidLimit(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_bounded_invalid")
	_, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "x", MaxOutputLength: -1})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Fatalf("err = %v", err)
	}
}

func TestPortToolsSessionRunBoundedAbortedBeforeStart(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_bounded_abort")
	reason := errors.New("cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	_, err := NewSession(sbx).Run(ctx, providerutils.SandboxProcessOptions{Command: "x", MaxOutputLength: 4})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v", err)
	}
}

// TS 8956ca55ed (vercel-sandbox.test.ts "names the template with templateName
// and the fork with its live name", "keeps the derived template name when
// templateName is absent", "ignores templateName when there is no template").
func TestPortToolsTemplateNameNamesTemplateAndLiveSandbox(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.snapshotOnStop = true
	prepared := 0
	template := &Template{Identity: "recipe-one", Prepare: func(context.Context, providerutils.SandboxSession) error {
		prepared++
		return nil
	}}
	create := func(opts CreateSessionOptions) {
		opts.Credentials, opts.BaseURL, opts.Runtime, opts.Template, opts.TemplateName = testCreds(), f.URL(), "node24", template, port2toolsStr("app-template")
		if _, err := CreateNetworkSandboxSession(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
	}
	create(CreateSessionOptions{SandboxID: "live-sandbox-id"})
	create(CreateSessionOptions{Name: "live-name"})

	var names []string
	for _, req := range f.createRequests {
		names = append(names, req.Name)
	}
	if got := strings.Join(names, ","); got != "app-template,live-sandbox-id,live-name" {
		t.Fatalf("created sandboxes = %s", got)
	}
	if prepared != 1 {
		t.Fatalf("prepared %d times, want once (second call reuses the named template)", prepared)
	}
}

func TestPortToolsTemplateNameAbsentKeepsDerivedName(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.snapshotOnStop = true
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), Runtime: "node24", SandboxID: "live",
		Template: &Template{Identity: "recipe-one", Prepare: func(context.Context, providerutils.SandboxSession) error { return nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) < 1 || !strings.HasPrefix(f.createRequests[0].Name, "ai-sdk-harness-v3-") {
		t.Fatalf("requests = %#v", f.createRequests)
	}
}

func TestPortToolsTemplateNameIgnoredWithoutTemplate(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), Runtime: "node24", SandboxID: "live-name", TemplateName: port2toolsStr("app-template"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) != 1 || f.createRequests[0].Name != "live-name" {
		t.Fatalf("requests = %#v", f.createRequests)
	}
}
