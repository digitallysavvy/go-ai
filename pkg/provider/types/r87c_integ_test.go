package types

import (
	"encoding/json"
	"testing"
)

// TestR87cintegReasoningFileTSShape checks the ModelMessage reasoning-file
// part shape of TS generate-text/to-response-messages.ts (`data:
// part.file.base64`, stream-text.test.ts 9135 response.messages), and that the
// TS shape and the v0.5 Go shape both read back (cr-r87c-stream-to-gen).
func TestR87cintegReasoningFileTSShape(t *testing.T) {
	cases := []struct {
		name string
		in   ReasoningFileContent
		want string
	}{
		{"data string", ReasoningFileContent{MediaType: "image/png", FileData: FileData{Type: FileDataTypeData, DataString: "abc"}},
			`{"type":"reasoning-file","data":"abc","mediaType":"image/png"}`},
		{"file data bytes", ReasoningFileContent{MediaType: "image/png", FileData: FileData{Type: FileDataTypeData, Data: []byte("hi")}},
			`{"type":"reasoning-file","data":"aGk=","mediaType":"image/png"}`},
		{"legacy bytes", ReasoningFileContent{MediaType: "image/png", Data: []byte("hi")},
			`{"type":"reasoning-file","data":"aGk=","mediaType":"image/png"}`},
		{"url", ReasoningFileContent{MediaType: "image/png", FileData: FileData{Type: FileDataTypeURL, URL: "https://example.com/a.png"}},
			`{"type":"reasoning-file","data":{"type":"url","url":"https://example.com/a.png"},"mediaType":"image/png"}`},
		{"provider options", ReasoningFileContent{MediaType: "image/jpeg", FileData: FileData{Type: FileDataTypeData, DataString: "x"},
			ProviderOptions: map[string]interface{}{"testProvider": map[string]interface{}{"signature": "rf-sig-1"}}},
			`{"type":"reasoning-file","data":"x","mediaType":"image/jpeg","providerOptions":{"testProvider":{"signature":"rf-sig-1"}}}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("%s: got %s, want %s", c.name, b, c.want)
		}
		var back ReasoningFileContent
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		again, _ := json.Marshal(back)
		if string(again) != c.want {
			t.Errorf("%s: round trip %s, want %s", c.name, again, c.want)
		}
	}

	// v0.5 Go shape: tagged fileData plus a base64 data string, or a null data.
	var v05 ReasoningFileContent
	if err := json.Unmarshal([]byte(`{"type":"reasoning-file","fileData":{"type":"data","data":"reasoning-file-data-1"},"mediaType":"image/png","data":null}`), &v05); err != nil {
		t.Fatal(err)
	}
	if v05.FileData.Type != FileDataTypeData || v05.FileData.DataString != "reasoning-file-data-1" || v05.MediaType != "image/png" {
		t.Fatalf("v0.5 shape read back as %+v", v05)
	}
	var v05b ReasoningFileContent
	if err := json.Unmarshal([]byte(`{"type":"reasoning-file","fileData":{"type":""},"mediaType":"image/png","data":"aGk="}`), &v05b); err != nil {
		t.Fatal(err)
	}
	if string(v05b.Data) != "hi" {
		t.Fatalf("v0.5 bytes read back as %q", v05b.Data)
	}
	// TS tagged data object.
	var tagged ReasoningFileContent
	if err := json.Unmarshal([]byte(`{"type":"reasoning-file","data":{"type":"data","data":"aGk="},"mediaType":"image/png"}`), &tagged); err != nil {
		t.Fatal(err)
	}
	if tagged.FileData.DataString != "aGk=" || string(tagged.Data) != "hi" {
		t.Fatalf("tagged shape read back as %+v", tagged)
	}
}
