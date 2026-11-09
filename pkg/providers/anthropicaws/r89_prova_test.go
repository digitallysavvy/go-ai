package anthropicaws

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR89provaRegionLoadSettingError: the SigV4 fetch loads the region with
// loadSetting at request time, so a missing region is a LoadSettingError
// ("... environment variable."), even with a custom baseURL (TS
// anthropic-aws-provider.ts).
func TestR89provaRegionLoadSettingError(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("ANTHROPIC_AWS_API_KEY", "")
	p, err := New(Config{BaseURL: "http://127.0.0.1:1/v1", WorkspaceID: "ws", AccessKeyID: "AKID", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var lse *providererrors.LoadSettingError
	if !errors.As(err, &lse) || lse.Error() != "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable." {
		t.Fatalf("err = %T %v", err, err)
	}
}
