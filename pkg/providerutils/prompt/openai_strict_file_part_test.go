package prompt

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func filePartMessage(f types.FileContent) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}, f}}}
}

// TS convert-to-openai-chat-messages.ts / convert-to-openai-compatible-chat-
// messages.ts / convert-to-groq-chat-messages.ts throw
// UnsupportedFunctionalityError for user file parts they cannot send.
func TestStrictFilePartErrors(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3}
	cases := []struct {
		name string
		mode FilePartMode
		file types.FileContent
		want string // "" means no error
	}{
		{"chat text file", FilePartsOpenAIChat, types.FileContent{MediaType: "text/plain", Data: []byte("hi")}, "file part media type text/plain"},
		{"chat pdf url", FilePartsOpenAIChat, types.FileContent{MediaType: "application/pdf", URL: "https://x/y.pdf"}, "PDF file parts with URLs"},
		{"chat audio url", FilePartsOpenAIChat, types.FileContent{MediaType: "audio/wav", URL: "https://x/y.wav"}, "audio file parts with URLs"},
		{"chat audio ogg", FilePartsOpenAIChat, types.FileContent{MediaType: "audio/ogg", Data: []byte("x")}, "audio content parts with media type audio/ogg"},
		{"chat bare image resolved", FilePartsOpenAIChat, types.FileContent{MediaType: "image", Data: png}, ""},
		{"chat bare image undetectable", FilePartsOpenAIChat, types.FileContent{MediaType: "image", Data: []byte("zz")}, "must specify subtype since it could not be auto-detected"},
		{"compat zip", FilePartsOpenAICompatible, types.FileContent{MediaType: "application/zip", Data: []byte("x")}, "file part media type application/zip"},
		{"compat pdf url", FilePartsOpenAICompatible, types.FileContent{MediaType: "application/pdf", URL: "https://x/y.pdf"}, "PDF file parts with URLs"},
		{"compat audio ogg", FilePartsOpenAICompatible, types.FileContent{MediaType: "audio/ogg", Data: []byte("x")}, "audio media type audio/ogg"},
		{"compat unknown top level", FilePartsOpenAICompatible, types.FileContent{MediaType: "font/woff", Data: []byte("x")}, "file part media type font/woff"},
		{"groq pdf", FilePartsGroq, types.FileContent{MediaType: "application/pdf", Data: []byte("x")}, "Non-image file content parts"},
		{"groq image url", FilePartsGroq, types.FileContent{MediaType: "image/png", URL: "https://x/y.png"}, ""},
	}
	for _, tc := range cases {
		_, err := ToOpenAIMessagesWithError(filePartMessage(tc.file), ToOpenAIMessagesOptions{FileParts: tc.mode})
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestStrictFilePartShapes(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3}
	out, err := ToOpenAIMessagesWithError(filePartMessage(types.FileContent{MediaType: "image", Data: png}), ToOpenAIMessagesOptions{FileParts: FilePartsOpenAIChat})
	if err != nil {
		t.Fatal(err)
	}
	img := out[0]["content"].([]map[string]interface{})[1]["image_url"].(map[string]interface{})
	if img["url"] != "data:image/png;base64,iVBORw0KGgoBAgM=" {
		t.Fatalf("bare image media type must be sniffed: %#v", img)
	}

	out, err = ToOpenAIMessagesWithError(filePartMessage(types.FileContent{MediaType: "text/markdown", Data: []byte("# hi")}), ToOpenAIMessagesOptions{FileParts: FilePartsOpenAICompatible, SpreadCompatMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	text := out[0]["content"].([]map[string]interface{})[1]
	if text["type"] != "text" || text["text"] != "# hi" {
		t.Fatalf("compat text/* file must be a text part: %#v", text)
	}

	out, err = ToOpenAIMessagesWithError(filePartMessage(types.FileContent{MediaType: "application/pdf", Data: []byte("%PDF")}), ToOpenAIMessagesOptions{FileParts: FilePartsOpenAICompatible, SpreadCompatMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	file := out[0]["content"].([]map[string]interface{})[1]["file"].(map[string]interface{})
	if file["filename"] != "document.pdf" || file["file_data"] != "data:application/pdf;base64,JVBERg==" {
		t.Fatalf("pdf: %#v", file)
	}

	// Legacy mode keeps the lenient generic part.
	if _, err := ToOpenAIMessagesWithError(filePartMessage(types.FileContent{MediaType: "text/plain", Data: []byte("hi")}), ToOpenAIMessagesOptions{}); err != nil {
		t.Fatalf("legacy mode must not error: %v", err)
	}
}
