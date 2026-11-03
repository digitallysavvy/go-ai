package mistral

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provaPNGBadTail is a PNG signature in standard base64 followed by a tail
// that is not base64: TS detectMediaType decodes only the sniffed prefix
// (decodePrefix: ceil(18/3)*4 characters), so the type is still detected and
// the string is forwarded as is (convertToBase64).
const r82provaPNGBadTail = "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAA!!not-base64!!"

// r82provaBadPrefix has a prefix atob rejects: TS detectMediaType throws.
const r82provaBadPrefix = "!!!!iVBORw0KGgoAAAAAAAAAAAAAAAA"

func r82provaDataFile(mediaType, data string) types.FileContent {
	return types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, MediaType: mediaType, DataString: data}}
}

func r82provaIsDecodeError(err error) bool {
	var unsupported *providererrors.UnsupportedFunctionalityError
	return err != nil && !errors.As(err, &unsupported)
}

func TestR82provaMistralSniffDecodesOnlyPrefix(t *testing.T) {
	got, err := convertMistralFilePart(r82provaDataFile("image", r82provaPNGBadTail))
	if err != nil || got["image_url"] != "data:image/png;base64,"+r82provaPNGBadTail {
		t.Fatalf("part = %v, %v", got, err)
	}
	if _, err := convertMistralFilePart(r82provaDataFile("image", r82provaBadPrefix)); !r82provaIsDecodeError(err) {
		t.Fatalf("bad prefix: err = %v", err)
	}
}

// TS mistralConversationUsageSchema defaults the three token counts to 0, so
// "usage": {} is zero tokens with raw {prompt_tokens: 0, completion_tokens:
// 0, total_tokens: 0} (TS wire replay "Mistral web search > handles string
// output, ..."); Go reported an empty raw object.
func TestR82provaMistralConversationUsageDefaults(t *testing.T) {
	u := convertMistralConversationUsage([]byte(`{}`))
	want := map[string]interface{}{"prompt_tokens": float64(0), "completion_tokens": float64(0), "total_tokens": float64(0)}
	if u.InputTokens == nil || *u.InputTokens != 0 || u.OutputTokens == nil || *u.OutputTokens != 0 || !reflect.DeepEqual(u.Raw, want) {
		t.Fatalf("usage = %+v", u)
	}
	u = convertMistralConversationUsage([]byte(`{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7,"x":1}`))
	if *u.InputTokens != 3 || u.Raw["x"] != float64(1) {
		t.Fatalf("usage = %+v", u)
	}
	if u = convertMistralConversationUsage(nil); u.InputTokens != nil {
		t.Fatalf("absent usage = %+v", u)
	}
}
