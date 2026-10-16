package alibaba

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS APICallError keeps an empty message from {"error":{"message":""}}.
func TestChatStatusErrorKeepsEmptyMessage(t *testing.T) {
	err := chatStatusError(400, nil, []byte(`{"error":{"message":"","code":"x"}}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("got %#v", perr)
	}
}
