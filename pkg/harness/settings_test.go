package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func finishEventsScript(func(string, interface{})) []StreamPart {
	return []StreamPart{
		&StreamStartPart{},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
	}
}

// TestNewAgent_NormalizesAndSnapshotsHeaders ports TS harness-agent.test.ts
// "normalizes and snapshots headers before passing them to doStart": header
// names are lower-cased, and the settings-time map is copied so a caller
// mutating its own map afterward does not affect what reaches doStart.
func TestNewAgent_NormalizesAndSnapshotsHeaders(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: finishEventsScript})
	headers := map[string]string{"X-Tenant": "acme"}

	a, err := NewAgent(AgentSettings{Harness: mock.harness, Headers: headers})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	headers["X-Tenant"] = "mutated"

	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = session.Destroy(context.Background()) }()

	adapter := mock.harness.(*mockHarnessAdapter)
	if len(adapter.startCalls) != 1 {
		t.Fatalf("startCalls = %+v, want 1", adapter.startCalls)
	}
	got := adapter.startCalls[0].Headers
	if got["x-tenant"] != "acme" {
		t.Fatalf("Headers[\"x-tenant\"] = %q, want %q (lower-cased, and captured before the caller's later mutation)", got["x-tenant"], "acme")
	}
}

// TestNewAgent_RejectsManagedHeaders ports TS harness-agent.test.ts's
// "rejects the managed header %s" table test: NewAgent must reject
// authorization/x-api-key/user-agent/x-client-app in any letter-casing, with
// the header name lower-cased in the error message.
func TestNewAgent_RejectsManagedHeaders(t *testing.T) {
	cases := []string{
		"authorization", "Authorization",
		"x-api-key", "X-API-Key",
		"user-agent", "User-Agent",
		"x-client-app", "X-Client-App",
	}
	for _, header := range cases {
		t.Run(header, func(t *testing.T) {
			mock := newMockHarness(mockHarnessOptions{script: finishEventsScript})
			_, err := NewAgent(AgentSettings{Harness: mock.harness, Headers: map[string]string{header: "caller-value"}})
			if err == nil {
				t.Fatalf("NewAgent: want an error for managed header %q, got nil", header)
			}
			wantMsg := "HarnessAgent: `headers` must not include the managed header `" + toLowerASCII(header) + "`."
			if err.Error() != wantMsg {
				t.Fatalf("NewAgent error = %q, want %q", err.Error(), wantMsg)
			}
		})
	}
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// TestNewAgent_NoHeadersIsFine verifies the zero-value (no Headers
// configured) case constructs successfully with no managed-header check
// firing.
func TestNewAgent_NoHeadersIsFine(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: finishEventsScript})
	if _, err := NewAgent(AgentSettings{Harness: mock.harness}); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
}

// TestNewAgent_RequiresHarness mirrors the constructor's other required-field
// validation.
func TestNewAgent_RequiresHarness(t *testing.T) {
	if _, err := NewAgent(AgentSettings{}); err == nil {
		t.Fatal("NewAgent: want an error when Harness is nil")
	}
}

// TestInstructionsText covers instructionsText's full contract: nil, a plain
// string, a types.Message/*types.Message system message (only Content text
// parts contribute, matching TS `.content` access on a SystemModelMessage —
// TS 4d1bf28), and an unsupported type producing an error instead of
// silently coercing to "".
func TestInstructionsText(t *testing.T) {
	tests := []struct {
		name    string
		in      interface{}
		want    string
		wantErr bool
	}{
		{name: "nil", in: nil, want: ""},
		{name: "string", in: "Be concise.", want: "Be concise."},
		{name: "empty string", in: "", want: ""},
		{
			name: "types.Message value",
			in: types.Message{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "Serve "}, types.TextContent{Text: "the user."}},
			},
			want: "Serve the user.",
		},
		{
			name: "*types.Message pointer",
			in: &types.Message{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "Be terse."}},
			},
			want: "Be terse.",
		},
		{name: "nil *types.Message pointer", in: (*types.Message)(nil), want: ""},
		{
			name: "message with no text parts",
			in:   types.Message{Role: types.RoleSystem, Content: []types.ContentPart{}},
			want: "",
		},
		{name: "unsupported type", in: 42, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := instructionsText(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("instructionsText(%#v) = %q, nil, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("instructionsText(%#v) error = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("instructionsText(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
