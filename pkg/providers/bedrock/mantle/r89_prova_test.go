package mantle

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR89provaRegionLoadSettingError: TS getBaseURL loads the region with
// loadSetting, so a missing region is a LoadSettingError whose text ends
// with "environment variable." (mantle/bedrock-mantle-provider.ts).
func TestR89provaRegionLoadSettingError(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	_, err := New(ProviderSettings{APIKey: "k"}).Chat("openai.gpt-oss-120b-1:0")
	var lse *providererrors.LoadSettingError
	if !errors.As(err, &lse) || err.Error() != "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable." {
		t.Fatalf("err = %T %v", err, err)
	}
}
