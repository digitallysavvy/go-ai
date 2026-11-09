package anthropic

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR89provaRegionLoadSettingError: the SigV4 fetch loads the region with
// loadSetting (TS amazon-bedrock-anthropic-provider.ts), so a missing region
// is a LoadSettingError even with a custom baseURL.
func TestR89provaRegionLoadSettingError(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	m, err := New(Config{BaseURL: "http://127.0.0.1:1", Credentials: &AWSCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"}}).LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var lse *providererrors.LoadSettingError
	if !errors.As(err, &lse) || lse.Error() != "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable." {
		t.Fatalf("err = %T %v", err, err)
	}
}
