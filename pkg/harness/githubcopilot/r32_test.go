package githubcopilot

import (
	"context"
	"errors"
	"testing"
)

// TS awaits findGitHubCliExecutable without a catch, so a rejection from the
// finder is the result of readGitHubCopilotSubscription.
func TestReadSubscriptionPropagatesFinderError(t *testing.T) {
	boom := errors.New("finder failed")
	_, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": t.TempDir()},
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
