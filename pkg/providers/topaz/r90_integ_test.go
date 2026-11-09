package topaz

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// tools r90 open item: TS throws AISDKError({name:
// 'TOPAZ_IMAGE_ENHANCEMENT_FAILED', message}) for a failed or cancelled job
// (topaz-image-model.ts 339-342), and InvalidResponseDataError({data,
// message}) when the download URL is missing (404-408).
func TestR90integImageNamedErrors(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"Failed"}`
	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	var te *Error
	if !errors.As(err, &te) || te.ErrorName() != "TOPAZ_IMAGE_ENHANCEMENT_FAILED" || te.Error() != "Topaz image enhancement failed for process "+processID+"." {
		t.Fatalf("err = %T %v, want *topaz.Error TOPAZ_IMAGE_ENHANCEMENT_FAILED", err, err)
	}

	ts2 := newImageTestServer(t)
	defer ts2.server.Close()
	ts2.downloadBody = `{}`
	_, err = ts2.model().DoGenerate(context.Background(), defaultImageOptions(ts2))
	var ire *providererrors.InvalidResponseDataError
	if !errors.As(err, &ire) || ire.Error() != "Topaz did not return a download URL for process "+processID+"." {
		t.Fatalf("err = %T %v, want InvalidResponseDataError", err, err)
	}
}
