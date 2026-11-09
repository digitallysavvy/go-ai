package anthropicaws

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS anthropic-aws-provider.ts getRegion / getHeaders: a missing region (no
// baseURL) or workspace ID is loadSetting's LoadSettingError.
func TestR90provaMissingSettingsAreLoadSettingErrors(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("ANTHROPIC_AWS_WORKSPACE_ID", "")
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{WorkspaceID: "w"}, "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable."},
		{Config{Region: "us-east-1"}, "Anthropic AWS workspace ID setting is missing. Pass it using the 'workspaceId' parameter or the ANTHROPIC_AWS_WORKSPACE_ID environment variable."},
	} {
		_, err := New(c.cfg)
		var lse *providererrors.LoadSettingError
		if !errors.As(err, &lse) || err.Error() != c.want {
			t.Errorf("err = %v, want %s", err, c.want)
		}
	}
}
