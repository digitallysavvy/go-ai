package google

import (
	"errors"
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

func TestR82provaGoogleInteractionsSniffDecodesOnlyPrefix(t *testing.T) {
	block, _, err := fileContentToInteractionBlock(r82provaDataFile("image", r82provaPNGBadTail), "")
	if err != nil || block["mime_type"] != "image/png" || block["data"] != r82provaPNGBadTail {
		t.Fatalf("block = %v, %v", block, err)
	}
	if _, _, err := fileContentToInteractionBlock(r82provaDataFile("image", r82provaBadPrefix), ""); !r82provaIsDecodeError(err) {
		t.Fatalf("bad prefix: err = %v", err)
	}
}
