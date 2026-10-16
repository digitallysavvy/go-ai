package klingai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS resolveKlingAIAuthToken throws a LoadAPIKeyError (and loadSetting's text
// for a missing half of the legacy pair); DoStart must not rewrap it.
func TestR34_MissingCredentialsAreLoadAPIKeyError(t *testing.T) {
	t.Setenv("KLINGAI_API_KEY", "")
	t.Setenv("KLINGAI_ACCESS_KEY", "")
	t.Setenv("KLINGAI_SECRET_KEY", "")
	_, err := resolveKlingAIAuthToken("", "", "")
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %T %v, want LoadAPIKeyError", err, err)
	}

	t.Setenv("KLINGAI_ACCESS_KEY", "ak")
	_, err = resolveKlingAIAuthToken("", "", "")
	want := "KlingAI secret key setting is missing. Pass it using the 'secretKey' parameter or the KLINGAI_SECRET_KEY environment variable."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestR34_DoStartDoesNotWrapLoadAPIKeyError(t *testing.T) {
	t.Setenv("KLINGAI_API_KEY", "")
	t.Setenv("KLINGAI_ACCESS_KEY", "")
	t.Setenv("KLINGAI_SECRET_KEY", "")
	m, err := newVideoModel(&Provider{}, "kling-v2.6-t2v")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %T %v, want unwrapped LoadAPIKeyError", err, err)
	}
	_, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"taskId":"t","endpointPath":"/v1/videos/text2video"}`)})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("status err = %T %v, want unwrapped LoadAPIKeyError", err, err)
	}
}
