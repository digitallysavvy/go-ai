package fx

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
)

// The subscription cases of TS fx-harness.test.ts, on the mock runtime.

type resolveCall struct{ required string }

func stubSubscription(h *libfxHarness, provider string, sub *fxSubscription) *[]resolveCall {
	var mu sync.Mutex
	calls := &[]resolveCall{}
	h.resolve = func(_ context.Context, _ AuthenticationMode, required string) (fxAuthentication, error) {
		mu.Lock()
		*calls = append(*calls, resolveCall{required})
		mu.Unlock()
		return fxAuthentication{Provider: provider, Subscription: sub}, nil
	}
	return calls
}

func staticSubscription(provider string) *fxSubscription {
	return &fxSubscription{
		provider: provider,
		read: func(context.Context) (libfx.Credentials, error) {
			return libfx.Credentials{AccessToken: "secret", AccountID: "account"}, nil
		},
		write: func(context.Context, libfx.Credentials) error { return nil },
	}
}

func TestLibfxSurfacesSelectedSubscriptionFailures(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			failure := errors.New("Selected credential store unavailable")
			sub := &fxSubscription{
				provider: "codex",
				read: func(context.Context) (libfx.Credentials, error) {
					if operation == "read" {
						return libfx.Credentials{}, failure
					}
					return libfx.Credentials{AccessToken: "secret", AccountID: "account"}, nil
				},
				write: func(context.Context, libfx.Credentials) error { return failure },
			}
			h, m := newMockHarness(t, Settings{Auth: harness.AuthMode(harness.AuthModeDirect)})
			calls := stubSubscription(h, "codex", sub)
			m.setRun(func(ctx context.Context, o runtimeOptions) error {
				if o.Provider != "codex" || o.Subscription == nil {
					return errors.New("Expected subscription")
				}
				var err error
				if operation == "read" {
					_, err = o.Subscription.Credentials(ctx, libfx.CredentialRequest{Provider: "codex"})
				} else {
					err = o.Subscription.OnCredentials(ctx, libfx.Credentials{AccessToken: "rotated"}, libfx.CredentialRequest{Provider: "codex"})
				}
				if err != nil {
					return errors.New("HostStreamFailed")
				}
				return nil
			})
			sb := newFakeSandbox()
			s, err := h.DoStart(context.Background(), startOpts(sb, "credential-failure"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DoDestroy(context.Background()) //nolint:errcheck
			log := &partLog{}
			err = doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("test"), Emit: log.emit})
			if !errors.Is(err, failure) {
				t.Fatalf("done = %v, want the store failure", err)
			}
			errs := log.ofType(harness.PartTypeError)
			if len(errs) != 1 || errs[0].(*harness.ErrorPart).Error != any(failure) {
				t.Fatalf("error parts = %+v", errs)
			}
			if len(*calls) != 1 || len(m.opts()) != 1 {
				t.Fatalf("resolves %d, runtimes %d", len(*calls), len(m.opts()))
			}
		})
	}
}

func TestLibfxPinsSubscriptionProviderAcrossResume(t *testing.T) {
	h, m := newMockHarness(t, Settings{Auth: harness.Authentication{}})
	calls := stubSubscription(h, "grok", staticSubscription("grok"))
	sb := newFakeSandbox()
	ctx := context.Background()
	start := startOpts(sb, "subscription-resume")
	s, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(ctx) //nolint:errcheck
	resumeFrom, err := s.DoStop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.allFiles(), `"provider":"grok"`) {
		t.Fatalf("state = %s", sb.allFiles())
	}
	start.ResumeFrom = resumeFrom
	resumed, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	if last := (*calls)[len(*calls)-1]; last.required != "grok" {
		t.Fatalf("resume resolved with required provider %q", last.required)
	}
	if err := doTurn(t, resumed, harness.PromptTurnOptions{Prompt: harness.TextPrompt("test")}); err != nil {
		t.Fatal(err)
	}
	if got := m.opts()[0].Provider; got != "grok" {
		t.Fatalf("runtime provider %q", got)
	}
	if strings.Contains(sb.allFiles(), "secret") {
		t.Fatal("the state file holds a credential")
	}
}

func TestLibfxForwardsModelsForSubscriptions(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		t.Run(provider, func(t *testing.T) {
			h, m := newMockHarness(t, Settings{Auth: harness.AuthMode(harness.AuthModeDirect)})
			stubSubscription(h, provider, staticSubscription(provider))
			sb := newFakeSandbox()
			s, err := h.DoStart(context.Background(), startOpts(sb, "subscription-model"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DoDestroy(context.Background()) //nolint:errcheck
			for _, model := range []string{"", "caller-supplied-model"} {
				if err := doTurn(t, s, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Model: model}, Prompt: harness.TextPrompt("test")}); err != nil {
					t.Fatal(err)
				}
			}
			opts := m.opts()
			if len(opts) != 2 {
				t.Fatalf("runtimes %d", len(opts))
			}
			for i, want := range []string{"", "caller-supplied-model"} {
				if opts[i].Provider != provider || opts[i].Model != want || opts[i].APIKey != "" || opts[i].Subscription == nil {
					t.Errorf("runtime %d = provider %q model %q apiKey set %v", i, opts[i].Provider, opts[i].Model, opts[i].APIKey != "")
				}
			}
		})
	}
}
