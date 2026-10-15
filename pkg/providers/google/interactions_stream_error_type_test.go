package google

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestInteractionsStreamMalformedEventCarriesJSONParseError(t *testing.T) {
	t.Parallel()
	body := "data: not json\n\n"
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	for _, c := range drainInteractions(t, s) {
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var jpe *providererrors.JSONParseError
		if !errors.As(c.Err, &jpe) || jpe.Text != "not json" {
			t.Fatalf("Err = %#v, want *JSONParseError", c.Err)
		}
		return
	}
	t.Fatal("no error chunk")
}
