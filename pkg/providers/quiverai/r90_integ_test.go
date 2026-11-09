package quiverai

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provc r90 V061 class (error data = TS schema output): a body that fails
// quiveraiErrorSchema has the status text and no data; a matching one has
// the schema output as data.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	err := quiverAIFailedResponseHandler(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"message":"bad"},"other":2}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Bad Request" || pe.Data != nil {
		t.Fatalf("off-schema body: %#v", err)
	}
	err = quiverAIFailedResponseHandler(&internalhttp.HTTPStatusError{StatusCode: 402, Body: []byte(`{"status":402,"code":"no_credits","message":"Out of credits","request_id":"r1","extra":1}`)})
	if !errors.As(err, &pe) || pe.Message != "Out of credits" || pe.Data != (quiverAPIError{Status: 402, Code: "no_credits", Message: "Out of credits", RequestID: "r1"}) {
		t.Fatalf("schema body: %#v", err)
	}
}
