package types

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// ProviderReference maps provider names to provider-specific file identifiers.
// It mirrors the TypeScript SDK's SharedV4ProviderReference shape.
type ProviderReference map[string]string

// FileDataType identifies which FileData field is populated.
type FileDataType string

const (
	FileDataTypeData      FileDataType = "data"
	FileDataTypeURL       FileDataType = "url"
	FileDataTypeReference FileDataType = "reference"
	FileDataTypeText      FileDataType = "text"
)

// FileData is the provider-facing tagged file data shape.
//
// The Type discriminator mirrors the TypeScript SDK's FileData union while
// keeping the Go representation as a flat struct for straightforward JSON
// round-trips.
type FileData struct {
	Type FileDataType `json:"type"`

	// Data holds raw bytes for Type == FileDataTypeData. encoding/json encodes
	// []byte as base64, matching Go's existing binary content behavior.
	Data []byte `json:"data,omitempty"`

	// DataString holds a base64-encoded data string for Type == FileDataTypeData.
	// This mirrors the TypeScript SDK's `{ type: "data", data: string }` upload
	// shape. Provider upload implementations decode it when constructing the
	// multipart payload.
	DataString string `json:"-"`

	// URL holds a remote http(s) URL for Type == FileDataTypeURL.
	URL string `json:"url,omitempty"`

	// Reference holds provider file references for Type == FileDataTypeReference.
	// It mirrors the TypeScript SDK's provider reference shape:
	// map provider name -> provider-specific file identifier.
	Reference ProviderReference `json:"reference,omitempty"`

	// Text holds inline text document content for Type == FileDataTypeText.
	Text string `json:"text,omitempty"`

	// MediaType is the IANA media type associated with the file data when known.
	MediaType string `json:"mediaType,omitempty"`
}

// MarshalJSON preserves the TypeScript file-data union shape. In particular,
// DataString is emitted as the "data" field for `{ type: "data" }` values.
func (f FileData) MarshalJSON() ([]byte, error) {
	type fileDataJSON struct {
		Type      FileDataType      `json:"type"`
		Data      interface{}       `json:"data,omitempty"`
		URL       string            `json:"url,omitempty"`
		Reference ProviderReference `json:"reference,omitempty"`
		Text      string            `json:"text,omitempty"`
		MediaType string            `json:"mediaType,omitempty"`
	}
	out := fileDataJSON{
		Type:      f.Type,
		URL:       f.URL,
		Reference: f.Reference,
		Text:      f.Text,
		MediaType: f.MediaType,
	}
	if f.DataString != "" {
		out.Data = f.DataString
	} else if len(f.Data) > 0 {
		out.Data = f.Data
	}
	return json.Marshal(out)
}

func (f FileData) IsZero() bool {
	return f.Type == "" &&
		len(f.Data) == 0 &&
		f.DataString == "" &&
		f.URL == "" &&
		len(f.Reference) == 0 &&
		f.Text == "" &&
		f.MediaType == ""
}

// DecodeFileDataString decodes the base64/base64url string variant accepted by
// the TypeScript SDK for file data.
func DecodeFileDataString(value string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(value)
	if decoded, err := base64.StdEncoding.DecodeString(normalized); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(normalized)
}

// ProviderReferenceString returns a stable single-provider reference for legacy
// Go fields that predate the provider-reference map shape.
func ProviderReferenceString(reference ProviderReference) string {
	if len(reference) == 0 {
		return ""
	}
	if ref := reference["openai"]; ref != "" {
		return ref
	}
	if ref := reference["anthropic"]; ref != "" {
		return ref
	}
	if ref := reference["google"]; ref != "" {
		return ref
	}
	if ref := reference["xai"]; ref != "" {
		return ref
	}
	for _, ref := range reference {
		if ref != "" {
			return ref
		}
	}
	return ""
}
