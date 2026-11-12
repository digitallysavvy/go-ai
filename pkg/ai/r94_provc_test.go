package ai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS getAudioMediaType lower-cases with String.prototype.toLowerCase, which
// maps U+0130 to "i" plus U+0307 (node: "audi̇o/mpeg"), so the type is not
// audio and the bytes are sniffed; strings.ToLower gave "audio/mpeg".
func TestR94provcAudioMediaTypeJSLowerCase(t *testing.T) {
	if got := getAudioMediaType("AUDİO/MPEG"); got != "" {
		t.Fatalf("getAudioMediaType = %q, want \"\" (not an audio type in TS)", got)
	}
	if got := getAudioMediaType(" Audio/MPEG ; x=1"); got != "audio/mpeg" {
		t.Fatalf("getAudioMediaType = %q, want audio/mpeg", got)
	}
}

// TS normalizeImageData: a DataContent string starting with http:// or
// https:// is a URL file (prompt image, frame image and reference), not a
// base64 payload that fails to decode.
func TestR94provcVideoHTTPDataStringIsURL(t *testing.T) {
	const u = "https://example.com/a.png"
	got, err := normalizeVideoInputs(
		VideoPrompt{Text: "p", Image: &VideoPromptImage{DataString: u}},
		[]VideoFrameImageInput{{Image: VideoPromptImage{DataString: "http://example.com/f.png"}, FrameType: "last_frame"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image == nil || got.Image.Type != "url" || got.Image.URL != u {
		t.Fatalf("image = %+v, want url %s", got.Image, u)
	}
	if len(got.FrameImages) != 1 || got.FrameImages[0].Image.Type != "url" || got.FrameImages[0].Image.URL != "http://example.com/f.png" {
		t.Fatalf("frame images = %+v", got.FrameImages)
	}
	refs, err := normalizeVideoInputs(VideoPrompt{Text: "p"}, nil,
		[]VideoReferenceInput{{Data: VideoPromptImage{DataString: u, MediaType: "video/mp4"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs.InputReferences) != 1 || refs.InputReferences[0].Type != "url" || refs.InputReferences[0].MediaType != "video/mp4" {
		t.Fatalf("references = %+v", refs.InputReferences)
	}
}

// TS convertDataContentToUint8Array throws InvalidDataContentError
// "Invalid data content. Content string is not a base64-encoded media."
func TestR94provcTranscribeInvalidBase64Text(t *testing.T) {
	_, err := decodeTranscriptionAudioBase64("not base64!!!")
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Field != "audio" || err.Error() != "Invalid data content. Content string is not a base64-encoded media." {
		t.Fatalf("err = %T %v", err, err)
	}
}
