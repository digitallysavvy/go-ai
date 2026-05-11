package types

import (
	"encoding/json"
	"testing"
)

func TestFileDataMarshalDataStringUsesTypeScriptShape(t *testing.T) {
	data := FileData{Type: FileDataTypeData, DataString: "aGVsbG8="}

	got, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != `{"type":"data","data":"aGVsbG8="}` {
		t.Fatalf("Marshal() = %s", got)
	}
}

func TestDecodeFileDataStringAcceptsBase64URL(t *testing.T) {
	got, err := DecodeFileDataString("-_8")
	if err != nil {
		t.Fatalf("DecodeFileDataString() error = %v", err)
	}
	if len(got) != 2 || got[0] != 0xfb || got[1] != 0xff {
		t.Fatalf("DecodeFileDataString() = %#v", got)
	}
}
