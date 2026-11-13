package groq

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS groqChatChunkSchema = z.union([data object, groqErrorDataSchema]): the
// data member strips an `error` key, so a valid chunk that also carries one
// streams as data (cr-r98-provb-to-provg 1). A frame without choices is
// still the error part.
func TestR98provgStreamErrorKeyOnDataChunk(t *testing.T) {
	for _, tc := range []struct {
		name, frame string
		wantText    string
		wantErr     bool
	}{
		{"null error", `{"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":null}`, "hi", false},
		{"object error", `{"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":{"message":"x","type":"t"}}`, "hi", false},
		{"error only", `{"error":{"message":"boom","type":"server_error"}}`, "", true},
	} {
		body := "data: " + tc.frame + "\n\ndata: [DONE]\n\n"
		m, err := New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, body, true)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		text, sawErr := "", false
		for {
			c, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			switch c.Type {
			case provider.ChunkTypeText:
				text += c.Text
			case provider.ChunkTypeError:
				sawErr = true
			}
		}
		if text != tc.wantText || sawErr != tc.wantErr {
			t.Errorf("%s: text=%q err=%v", tc.name, text, sawErr)
		}
	}
}
