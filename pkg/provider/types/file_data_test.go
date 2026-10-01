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

func TestFileDataMarshalIncludesActiveTypeScriptUnionField(t *testing.T) {
	tests := []struct {
		name string
		data FileData
		want string
	}{
		{
			name: "empty data string",
			data: FileData{Type: FileDataTypeData},
			want: `{"type":"data","data":""}`,
		},
		{
			name: "empty URL",
			data: FileData{Type: FileDataTypeURL},
			want: `{"type":"url","url":""}`,
		},
		{
			name: "empty reference",
			data: FileData{Type: FileDataTypeReference},
			want: `{"type":"reference","reference":{}}`,
		},
		{
			name: "empty text",
			data: FileData{Type: FileDataTypeText},
			want: `{"type":"text","text":""}`,
		},
		{
			name: "reference map",
			data: FileData{Type: FileDataTypeReference, Reference: ProviderReference{"openai": "file-123"}},
			want: `{"type":"reference","reference":{"openai":"file-123"}}`,
		},
		{
			name: "media type stays outer file metadata",
			data: FileData{Type: FileDataTypeURL, URL: "https://example.com/file.pdf", MediaType: "application/pdf"},
			want: `{"type":"url","url":"https://example.com/file.pdf"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", got, tt.want)
			}
		})
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
