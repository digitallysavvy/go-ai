package xai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/require"
)

// TS createOpenAICompatible({name: 'googleVertex.xai'}) resolves provider file
// references with the "googleVertex" key (TS #22466).
func TestLanguageModel_FileReference(t *testing.T) {
	prov, err := New(Config{Project: "p", AccessToken: "t"})
	require.NoError(t, err)
	mdl, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	lm := mdl.(*LanguageModel)
	mk := func(ref types.ProviderReference) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				MediaType: "application/pdf",
				FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: ref},
			}},
		}}}}
	}
	body := lm.buildRequestBody(mk(types.ProviderReference{"googleVertex": "file-9"}), false)
	msgs := body["messages"].([]map[string]interface{})
	parts := msgs[0]["content"].([]map[string]interface{})
	require.Equal(t, map[string]interface{}{"file_id": "file-9"}, parts[0]["file"])

	var noSuch *providererrors.NoSuchProviderReferenceError
	_, err = lm.DoGenerate(context.Background(), mk(types.ProviderReference{"other": "x"}))
	require.True(t, errors.As(err, &noSuch), "err=%v", err)
	_, err = lm.DoStream(context.Background(), mk(types.ProviderReference{"other": "x"}))
	require.True(t, errors.As(err, &noSuch), "err=%v", err)
}
