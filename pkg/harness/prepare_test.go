package harness

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func makeRecipe(id string) *Bootstrap {
	return &Bootstrap{
		HarnessID:    id,
		BootstrapDir: ".harness-bootstrap/" + id,
		Files:        []BootstrapFile{{Path: ".harness-bootstrap/" + id + "/file.txt", Content: id}},
		Commands:     []BootstrapCommand{{Command: "echo " + id}},
	}
}

func TestPrepareSandboxForHarness(t *testing.T) {
	ctx := context.Background()

	t.Run("prepares multiple harnesses in sorted order and returns metadata", func(t *testing.T) {
		sb := newMockSandbox()
		alpha, beta := makeRecipe("alpha"), makeRecipe("beta")
		var bootstrapCtx SandboxBootstrapContext
		result, err := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{
			Session:   sb,
			Harnesses: []Harness{makeHarness("beta", beta), makeHarness("alpha", alpha)},
			SandboxConfig: &SandboxConfig{
				WorkDir:       "repo",
				BootstrapHash: "repo-v1",
				OnBootstrap: func(_ context.Context, bc SandboxBootstrapContext) error {
					bootstrapCtx = bc
					return nil
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !hex16.MatchString(result.Identity) {
			t.Fatal(result.Identity)
		}
		if !reflect.DeepEqual(result.RecipeIdentities, map[string]string{"alpha": HashHarnessBootstrap(*alpha), "beta": HashHarnessBootstrap(*beta)}) {
			t.Fatal(result.RecipeIdentities)
		}
		if len(result.SkippedHarnessIDs) != 0 {
			t.Fatal(result.SkippedHarnessIDs)
		}
		want := []string{`printf "%s" "$HOME"`, `mkdir -p "$BOOTSTRAP_DIR"`, "echo alpha", `mkdir -p "$BOOTSTRAP_DIR"`, "echo beta", "pwd", `mkdir -p "$WORK_DIR"`}
		if got := sb.runCommands(); !reflect.DeepEqual(got, want) {
			t.Fatalf("commands = %q", got)
		}
		if bootstrapCtx.WorkDir != "/work/repo" || bootstrapCtx.Session != sb {
			t.Fatalf("bootstrap ctx = %+v", bootstrapCtx)
		}
	})

	t.Run("returns the same identity for the same harnesses in a different order", func(t *testing.T) {
		alpha := makeHarness("alpha", makeRecipe("alpha"))
		beta := makeHarness("beta", makeRecipe("beta"))
		first, _ := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: newMockSandbox(), Harnesses: []Harness{alpha, beta}, SandboxConfig: &SandboxConfig{WorkDir: "repo"}})
		second, _ := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: newMockSandbox(), Harnesses: []Harness{beta, alpha}, SandboxConfig: &SandboxConfig{WorkDir: "repo"}})
		if first.Identity != second.Identity || !reflect.DeepEqual(first.RecipeIdentities, second.RecipeIdentities) {
			t.Fatal("identity must be order independent")
		}
	})

	t.Run("skips harnesses without bootstrap recipes", func(t *testing.T) {
		sb := newMockSandbox()
		result, err := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: sb, Harnesses: []Harness{makeHarness("pi", nil)}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Identity != "" || len(result.RecipeIdentities) != 0 || !reflect.DeepEqual(result.SkippedHarnessIDs, []string{"pi"}) {
			t.Fatalf("result = %+v", result)
		}
		if len(sb.runs) != 0 {
			t.Fatal("no commands expected")
		}
	})

	t.Run("deduplicates harnesses by id and uses the last adapter", func(t *testing.T) {
		second := makeRecipe("alpha")
		second.Commands = []BootstrapCommand{{Command: "echo second"}}
		sb := newMockSandbox()
		result, err := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{
			Session:   sb,
			Harnesses: []Harness{makeHarness("alpha", makeRecipe("alpha")), makeHarness("alpha", second)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !hex16.MatchString(result.Identity) || !reflect.DeepEqual(result.RecipeIdentities, map[string]string{"alpha": HashHarnessBootstrap(*second)}) {
			t.Fatalf("result = %+v", result)
		}
		if got := sb.runCommands(); !reflect.DeepEqual(got, []string{`printf "%s" "$HOME"`, `mkdir -p "$BOOTSTRAP_DIR"`, "echo second"}) {
			t.Fatalf("commands = %q", got)
		}
	})

	t.Run("validates caller bootstrap settings", func(t *testing.T) {
		_, err := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: newMockSandbox(), Harnesses: []Harness{makeHarness("alpha", nil)}, SandboxConfig: &SandboxConfig{OnBootstrap: noopBootstrap}})
		if err == nil || !strings.Contains(err.Error(), "must be provided together") {
			t.Fatal(err)
		}
		_, err = PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: newMockSandbox(), Harnesses: []Harness{makeHarness("alpha", nil)}, SandboxConfig: &SandboxConfig{WorkDir: "../repo"}})
		if err == nil || !strings.Contains(err.Error(), "workDir") {
			t.Fatal(err)
		}
		_, err = PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{Session: newMockSandbox()})
		if err == nil || !strings.Contains(err.Error(), "at least one harness") {
			t.Fatal(err)
		}
	})

	t.Run("accepts a full sandboxConfig and ignores onSession", func(t *testing.T) {
		called := false
		_, err := PrepareSandboxForHarness(ctx, PrepareSandboxForHarnessOptions{
			Session:   newMockSandbox(),
			Harnesses: []Harness{makeHarness("alpha", makeRecipe("alpha"))},
			SandboxConfig: &SandboxConfig{OnSession: func(context.Context, SandboxSessionContext) error {
				called = true
				return nil
			}},
		})
		if err != nil || called {
			t.Fatal("onSession must not be called", err)
		}
	})
}

type fakeProvider struct {
	session  *mockNetworkSandbox
	opts     []CreateSandboxSessionOptions
	runFirst bool
}

func (p *fakeProvider) SpecificationVersion() string { return SandboxSpecificationVersion }
func (p *fakeProvider) ProviderID() string           { return "mock-sandbox" }
func (p *fakeProvider) CreateSession(ctx context.Context, opts CreateSandboxSessionOptions) (NetworkSandboxSession, error) {
	p.opts = append(p.opts, opts)
	if p.runFirst && opts.OnFirstCreate != nil {
		if err := opts.OnFirstCreate(ctx, p.session); err != nil {
			return nil, err
		}
	}
	return p.session, nil
}

func TestPrepareHarnessSandboxTemplate(t *testing.T) {
	ctx := context.Background()

	t.Run("runs caller bootstrap through provider onFirstCreate and stops the session", func(t *testing.T) {
		provider := &fakeProvider{session: &mockNetworkSandbox{mockSandbox: newMockSandbox()}, runFirst: true}
		var bc SandboxBootstrapContext
		err := PrepareHarnessSandboxTemplate(ctx, PrepareHarnessSandboxTemplateOptions{
			Harness:         makeHarness("mock", nil),
			SandboxProvider: provider,
			SandboxConfig: &SandboxConfig{WorkDir: "ai-sdk", BootstrapHash: "repo-v1", OnBootstrap: func(_ context.Context, c SandboxBootstrapContext) error {
				bc = c
				return nil
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(provider.opts) != 1 || !hex16.MatchString(provider.opts[0].Identity) || provider.opts[0].OnFirstCreate == nil || provider.opts[0].SessionID != "" {
			t.Fatalf("createSession opts = %+v", provider.opts)
		}
		if bc.WorkDir != "/work/ai-sdk" {
			t.Fatal(bc.WorkDir)
		}
		if provider.session.stops != 1 {
			t.Fatalf("stops = %d", provider.session.stops)
		}
	})

	t.Run("applies the harness bootstrap recipe under the sandbox HOME", func(t *testing.T) {
		provider := &fakeProvider{session: &mockNetworkSandbox{mockSandbox: newMockSandbox()}}
		recipe := &Bootstrap{
			HarnessID:    "mock",
			BootstrapDir: ".harness-bootstrap/mock",
			Files:        []BootstrapFile{{Path: ".harness-bootstrap/mock/bridge.mjs", Content: "x"}},
		}
		if err := PrewarmHarness(ctx, PrepareHarnessSandboxTemplateOptions{Harness: makeHarness("mock", recipe), SandboxProvider: provider}); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range provider.session.writes {
			if w.Path == "/home/agent/.ai-sdk-harness/.harness-bootstrap/mock/bridge.mjs" {
				found = true
			}
		}
		if !found {
			t.Fatalf("writes = %+v", provider.session.writes)
		}
	})

	t.Run("no-op without recipe or caller bootstrap", func(t *testing.T) {
		provider := &fakeProvider{session: &mockNetworkSandbox{mockSandbox: newMockSandbox()}}
		if err := PrepareHarnessSandboxTemplate(ctx, PrepareHarnessSandboxTemplateOptions{Harness: makeHarness("mock", nil), SandboxProvider: provider}); err != nil || len(provider.opts) != 0 {
			t.Fatal("expected no sandbox creation", err)
		}
	})
}
