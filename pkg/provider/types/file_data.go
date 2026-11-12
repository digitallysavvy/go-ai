package types

import (
	"encoding/base64"
	"encoding/json"
	"io"
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

	// FileDataTypeStream identifies an upload data variant containing a byte
	// stream. Providers that support streaming uploads read directly from
	// Stream without buffering the full file in memory. Mirrors TypeScript's
	// FilesV4UploadFileStreamData ({ type: 'stream', stream }).
	FileDataTypeStream FileDataType = "stream"
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

	// URL holds the file URL for Type == FileDataTypeURL. Core prompt
	// conversion replaces it with its WHATWG serialization (TS
	// url.toString()), which providers send.
	URL string `json:"url,omitempty"`

	// OriginalURL holds the URL as the caller wrote it when that differs
	// from the normalized URL, for Type == FileDataTypeURL. Core prompt
	// conversion sets it for the legacy URL string fields
	// (FileContent.URL, ImageContent.URL, FileContentBlock.URL); a tagged
	// FileData URL keeps the OriginalURL its caller set. Mirrors TS
	// `{ type: 'url', url, originalUrl? }`; Google sends it as given.
	OriginalURL string `json:"originalUrl,omitempty"`

	// Reference holds provider file references for Type == FileDataTypeReference.
	// It mirrors the TypeScript SDK's provider reference shape:
	// map provider name -> provider-specific file identifier.
	Reference ProviderReference `json:"reference,omitempty"`

	// Text holds inline text document content for Type == FileDataTypeText.
	Text string `json:"text,omitempty"`

	// MediaType is the IANA media type associated with the file data when known.
	MediaType string `json:"mediaType,omitempty"`

	// Stream holds a byte stream to read upload data from, for Type ==
	// FileDataTypeStream. Not JSON-serializable; provider upload
	// implementations read directly from it (e.g. as a multipart body part)
	// instead of buffering the full file in memory.
	Stream io.Reader `json:"-"`
}

// MarshalJSON preserves the TypeScript file-data union shape. In particular,
// DataString is emitted as the "data" field for `{ type: "data" }` values.
func (f FileData) MarshalJSON() ([]byte, error) {
	type fileDataBase struct {
		Type FileDataType `json:"type"`
	}
	base := fileDataBase{Type: f.Type}
	switch f.Type {
	case FileDataTypeData:
		value := interface{}("")
		if f.DataString != "" {
			value = f.DataString
		} else if f.Data != nil {
			value = f.Data
		}
		return json.Marshal(struct {
			fileDataBase
			Data interface{} `json:"data"`
		}{
			fileDataBase: base,
			Data:         value,
		})
	case FileDataTypeURL:
		return json.Marshal(struct {
			fileDataBase
			URL         string `json:"url"`
			OriginalURL string `json:"originalUrl,omitempty"`
		}{
			fileDataBase: base,
			URL:          f.URL,
			OriginalURL:  f.OriginalURL,
		})
	case FileDataTypeReference:
		reference := f.Reference
		if reference == nil {
			reference = ProviderReference{}
		}
		return json.Marshal(struct {
			fileDataBase
			Reference ProviderReference `json:"reference"`
		}{
			fileDataBase: base,
			Reference:    reference,
		})
	case FileDataTypeText:
		return json.Marshal(struct {
			fileDataBase
			Text string `json:"text"`
		}{
			fileDataBase: base,
			Text:         f.Text,
		})
	default:
		type fileDataJSON struct {
			Type      FileDataType      `json:"type"`
			Data      interface{}       `json:"data,omitempty"`
			URL       string            `json:"url,omitempty"`
			Reference ProviderReference `json:"reference,omitempty"`
			Text      string            `json:"text,omitempty"`
		}
		out := fileDataJSON{
			Type:      f.Type,
			URL:       f.URL,
			Reference: f.Reference,
			Text:      f.Text,
		}
		if f.DataString != "" {
			out.Data = f.DataString
		} else if len(f.Data) > 0 {
			out.Data = f.Data
		}
		return json.Marshal(out)
	}
}

func (f FileData) IsZero() bool {
	return f.Type == "" &&
		len(f.Data) == 0 &&
		f.DataString == "" &&
		f.URL == "" &&
		f.OriginalURL == "" &&
		len(f.Reference) == 0 &&
		f.Text == "" &&
		f.MediaType == "" &&
		f.Stream == nil
}

// DecodeFileDataString decodes the base64/base64url string variant accepted by
// the TypeScript SDK for file data, like TS convertBase64ToUint8Array:
// base64url characters are mapped, then the string is decoded with atob's
// WHATWG forgiving-base64 rules (ASCII whitespace ignored, up to two
// trailing "=" dropped when the length is a multiple of 4, any other "=" or
// a length of 1 mod 4 rejected). The same rules as
// fileutil.DecodeForgivingBase64, which this package cannot import.
func DecodeFileDataString(value string) ([]byte, error) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, value)
	if len(compact)%4 == 0 {
		if strings.HasSuffix(compact, "==") {
			compact = compact[:len(compact)-2]
		} else if strings.HasSuffix(compact, "=") {
			compact = compact[:len(compact)-1]
		}
	}
	if strings.Contains(compact, "=") || len(compact)%4 == 1 {
		return nil, base64.CorruptInputError(0)
	}
	return base64.RawStdEncoding.DecodeString(compact)
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
