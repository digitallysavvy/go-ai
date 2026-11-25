package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r82integConvert(t *testing.T, mode FilePartMode, part types.FileContent) (string, error) {
	t.Helper()
	msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{part}}}
	out, err := ToOpenAIMessagesWithError(msgs, ToOpenAIMessagesOptions{FileParts: mode})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func r82integStringPart(mediaType, s string) types.FileContent {
	return types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: s, MediaType: mediaType}}
}

// TS convert-to-groq-chat-messages.ts 63, convert-to-openai-chat-messages.ts
// 164/179/216 and convert-to-openai-compatible-chat-messages.ts 79/92/105/130:
// a string payload is sent as given (convertToBase64 returns it), a bare type
// is sniffed from the string's prefix only (decodePrefix), and a compat text
// part decodes the string (prova CR r82 item 1).
func TestR82integStrictFilePartDataString(t *testing.T) {
	png := "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAA!!x" // bad tail, PNG prefix
	cases := []struct {
		name string
		mode FilePartMode
		part types.FileContent
		want string
	}{
		{"groq bare image sniffed from prefix", FilePartsGroq, r82integStringPart("image", png), `"url":"data:image/png;base64,` + png + `"`},
		{"groq base64url kept", FilePartsGroq, r82integStringPart("image/png", "-_8"), `"url":"data:image/png;base64,-_8"`},
		{"openai chat image", FilePartsOpenAIChat, r82integStringPart("image/*", png), `"url":"data:image/png;base64,` + png + `"`},
		{"openai chat audio", FilePartsOpenAIChat, r82integStringPart("audio/wav", "UklG"), `"input_audio":{"data":"UklG","format":"wav"}`},
		{"openai chat pdf", FilePartsOpenAIChat, r82integStringPart("application/pdf", "JVBERi0"), `"file_data":"data:application/pdf;base64,JVBERi0"`},
		{"compat image", FilePartsOpenAICompatible, r82integStringPart("image/png", "-_8"), `"url":"data:image/png;base64,-_8"`},
		{"compat video", FilePartsOpenAICompatible, r82integStringPart("video/mp4", "AAAA"), `"video_url":{"url":"data:video/mp4;base64,AAAA"}`},
		{"compat audio", FilePartsOpenAICompatible, r82integStringPart("audio/mpeg", "SUQz"), `"input_audio":{"data":"SUQz","format":"mp3"}`},
		{"compat pdf sniffed", FilePartsOpenAICompatible, r82integStringPart("application", "JVBERi0xLjQKJcfsj6IKNSAwIG9i"), `"file_data":"data:application/pdf;base64,JVBERi0xLjQKJcfsj6IKNSAwIG9i"`},
		{"compat text decoded", FilePartsOpenAICompatible, r82integStringPart("text/plain", "aGVsbG8"), `"text":"hello"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := r82integConvert(t, c.mode, c.part)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("got %s\nwant it to contain %s", got, c.want)
			}
		})
	}
}

// TS atob throws on a bad sniffed prefix; an undetectable prefix is the
// "could not be auto-detected" error.
func TestR82integStrictFilePartDataStringSniffErrors(t *testing.T) {
	if _, err := r82integConvert(t, FilePartsGroq, r82integStringPart("image", "!!!!")); err == nil {
		t.Fatal("bad base64 prefix: want an error")
	}
	_, err := r82integConvert(t, FilePartsGroq, r82integStringPart("image", "AAAAAAAA"))
	if err == nil || !strings.Contains(err.Error(), "could not be auto-detected") {
		t.Fatalf("undetected: err = %v", err)
	}
	if _, err := r82integConvert(t, FilePartsOpenAICompatible, r82integStringPart("text/plain", "!!")); err == nil {
		t.Fatal("compat text with an undecodable string: want an error")
	}
}
