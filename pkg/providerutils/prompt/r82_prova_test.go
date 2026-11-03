package prompt

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provaPNGBadTail is a PNG signature in standard base64 followed by a tail
// that is not base64: TS detectMediaType decodes only the sniffed prefix
// (decodePrefix: ceil(18/3)*4 characters), so the type is still detected.
const r82provaPNGBadTail = "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAA!!not-base64!!"

// r82provaBadPrefix has a prefix atob rejects (TS throws from detectMediaType).
const r82provaBadPrefix = "!!!!iVBORw0KGgoAAAAAAAAAAAAAAAA"

func r82provaFile(mediaType, data string) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}},
	}}}
}

func TestR82provaAnthropicSniffDecodesOnlyPrefix(t *testing.T) {
	out, err := ConvertToAnthropicPrompt(r82provaFile("image", r82provaPNGBadTail), AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Messages)
	if !strings.Contains(string(b), `"media_type":"image/png"`) || !strings.Contains(string(b), `"data":"`+r82provaPNGBadTail+`"`) {
		t.Fatalf("messages %s", b)
	}
	_, err = ConvertToAnthropicPrompt(r82provaFile("image", r82provaBadPrefix), AnthropicPromptOptions{})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if err == nil || errors.As(err, &unsupported) {
		t.Fatalf("bad prefix: err = %v, want the base64 decode error", err)
	}
}

// TS: topLevel === 'application' && resolveFullMediaType({part}) ===
// 'application/pdf'; resolveFullMediaType throws for undetectable bytes, so a
// bare "application" text payload fails with "could not be auto-detected",
// not "media type: application" (Go swallowed the resolve error).
func TestR82provaAnthropicApplicationUndetectedThrows(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "application", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("hello world, plain text")}},
	}}}
	_, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != `file of media type "application" must specify subtype since it could not be auto-detected` {
		t.Fatalf("err = %v", err)
	}
	// A PDF with a bad tail is still sniffed as a PDF document.
	out, err := ConvertToAnthropicPrompt(r82provaFile("application", "JVBERi0xLjQAAAAAAAAAAAAAAAAAAAAA**"), AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(out.Messages); !strings.Contains(string(b), `"media_type":"application/pdf"`) {
		t.Fatalf("messages %s", b)
	}
}

func TestR82provaGoogleSniffDecodesOnlyPrefix(t *testing.T) {
	got, err := ConvertToGoogleMessages(r82provaFile("image", r82provaPNGBadTail), GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"mimeType":"image/png"`) || !strings.Contains(string(b), `"data":"`+r82provaPNGBadTail+`"`) {
		t.Fatalf("prompt %s", b)
	}
	_, err = ConvertToGoogleMessages(r82provaFile("image", r82provaBadPrefix), GoogleMessagesOptions{})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if err == nil || errors.As(err, &unsupported) {
		t.Fatalf("bad prefix: err = %v, want the base64 decode error", err)
	}
}
