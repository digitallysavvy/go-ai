package types

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

	// URL holds a remote http(s) URL for Type == FileDataTypeURL.
	URL string `json:"url,omitempty"`

	// Reference holds a provider file reference for Type == FileDataTypeReference.
	Reference string `json:"reference,omitempty"`

	// Text holds inline text document content for Type == FileDataTypeText.
	Text string `json:"text,omitempty"`

	// MediaType is the IANA media type associated with the file data when known.
	MediaType string `json:"mediaType,omitempty"`
}

func (f FileData) IsZero() bool {
	return f.Type == "" &&
		len(f.Data) == 0 &&
		f.URL == "" &&
		f.Reference == "" &&
		f.Text == "" &&
		f.MediaType == ""
}
