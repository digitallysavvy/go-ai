package openresponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r82provbPNG = "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%"

// TS convertToOpenResponsesInput: a data part given as a base64 string is
// sent with that string (convertToBase64 returns it as is), its type sniffed
// from the prefix; v0.6 before this dropped such parts.
func TestR82provbOpenResponsesDataStringParts(t *testing.T) {
	var warnings []types.Warning
	out, err := convertUserContent([]types.ContentPart{
		types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}},
		types.FileContent{MediaType: "application/pdf", Filename: "a.pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "JVBERi0x"}},
	}, &warnings, "openresponses")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	for _, want := range []string{`"image_url":"data:image/png;base64,` + r82provbPNG + `"`, `"file_data":"data:application/pdf;base64,JVBERi0x"`} {
		if !strings.Contains(strings.ReplaceAll(string(b), `&`, "&"), want) {
			t.Fatalf("input %s lacks %s", b, want)
		}
	}
	part, ok, err := convertToolFileContentBlock(types.FileContentBlock{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}}, &warnings, "openresponses")
	if err != nil || !ok {
		t.Fatalf("tool block: ok=%v err=%v", ok, err)
	}
	if img, _ := part.(InputImageContent); img.ImageURL != "data:image/png;base64,"+r82provbPNG {
		t.Fatalf("tool block = %+v", part)
	}
}
