package topaz

import (
	"context"
	"errors"
	"reflect"
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

// provc r90 V061 class (error data = TS schema output): topazErrorDataSchema
// output (declared keys present) for a matching body, undefined otherwise.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	var pe *providererrors.ProviderError
	err := newTopazAPIError(400, []byte(`{"message":"m","errorCode":"E1","other":2}`), nil)
	if !errors.As(err, &pe) || pe.Message != "m (E1)" || !reflect.DeepEqual(pe.Data, map[string]interface{}{"message": "m", "errorCode": "E1"}) {
		t.Fatalf("schema body: %#v", err)
	}
	err = newTopazAPIError(400, []byte(`{"error":{"message":"bad"}}`), nil)
	if !errors.As(err, &pe) || pe.Message != "Bad Request" || pe.Data != nil {
		t.Fatalf("off-schema body: %#v", err)
	}
}
