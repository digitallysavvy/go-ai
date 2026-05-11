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
	Reference map[string]string `json:"reference,omitempty"`

	// Text holds inline text document content for Type == FileDataTypeText.
	Text string `json:"text,omitempty"`

	// MediaType is the IANA media type associated with the file data when known.
	MediaType string `json:"mediaType,omitempty"`
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

// ProviderReferenceString returns a stable single-provider reference for legacy
// Go fields that predate the provider-reference map shape.
func ProviderReferenceString(reference map[string]string) string {
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
