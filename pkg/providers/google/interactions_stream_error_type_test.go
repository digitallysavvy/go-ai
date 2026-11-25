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

func TestInteractionsStreamErrorEventCarriesStreamProviderError(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, msg string }{
		{`{"event_type":"error","error":{"code":"bad","message":"boom"}}`, "boom"},
		{`{"event_type":"error"}`, "Unknown interaction error"},
	}
	for _, tc := range cases {
		s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader("data: "+tc.body+"\n\n")), "", nil, nil, nil, 0)
		var found bool
		for _, c := range drainInteractions(t, s) {
			if c.Type != provider.ChunkTypeError {
				continue
			}
			found = true
			var spe *providererrors.StreamProviderError
			if !errors.As(c.Err, &spe) || spe.Message != tc.msg || spe.Type != "error" || c.Text != tc.msg {
				t.Fatalf("Err = %#v text=%q, want StreamProviderError %q", c.Err, c.Text, tc.msg)
			}
			if m, ok := spe.Data.(map[string]interface{}); !ok || m["event_type"] != "error" {
				t.Fatalf("Data = %#v, want raw event", spe.Data)
			}
			if tc.msg == "boom" && spe.Code != "bad" {
				t.Fatalf("Code = %#v", spe.Code)
			}
		}
		if !found {
			t.Fatal("no error chunk")
		}
	}
}
