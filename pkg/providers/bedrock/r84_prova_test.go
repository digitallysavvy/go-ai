package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR84provaConverseEventDataTextDecoder: TS
// createAmazonBedrockEventStreamDecoder hands safeParseJSON
// textDecoder.decode(body): a leading BOM is dropped and a truncated UTF-8
// sequence is one U+FFFD (Go's JSON decoder rejected the BOM and wrote one
// U+FFFD per invalid byte).
func TestR84provaConverseEventDataTextDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", "\xEF\xBB\xBF" + `{"contentBlockIndex":0,"delta":{"text":"a"}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"b` + "\xE2\x82" + `c"}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"end_turn"}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "amazon.nova-pro-v1:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text := ""
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeError:
			t.Errorf("error part: %v", c.Err)
		}
	}
	if want := "ab�c"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}
