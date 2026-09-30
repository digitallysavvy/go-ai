package types

import "time"

// UploadFileOptions are the provider-facing options for a single file upload.
type UploadFileOptions struct {
	Data            FileData               `json:"data"`
	MediaType       string                 `json:"mediaType,omitempty"`
	Filename        string                 `json:"filename,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// Headers are additional HTTP headers to send with the upload request.
	// Only applicable for HTTP-based providers. Mirrors TypeScript's
	// FilesV4UploadFileCallOptions.headers.
	Headers map[string]string `json:"-"`
}

// UploadSkillFile is a single file entry in a skill upload.
type UploadSkillFile struct {
	Path string   `json:"path"`
	Data FileData `json:"data"`
}

// UploadSkillOptions are the provider-facing options for skill upload.
type UploadSkillOptions struct {
	Files           []UploadSkillFile      `json:"files"`
	DisplayTitle    string                 `json:"displayTitle,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// UploadFileResult is returned by provider Files API implementations.
type UploadFileResult struct {
	ProviderReference ProviderReference      `json:"providerReference"`
	MediaType         string                 `json:"mediaType,omitempty"`
	Filename          string                 `json:"filename,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
	Warnings          []Warning              `json:"warnings"`

	// ByteSize is the size of the uploaded file in bytes, if available from
	// the provider.
	ByteSize *int64 `json:"byteSize,omitempty"`

	// CreatedAt is when the file was created, if available from the provider.
	CreatedAt *time.Time `json:"createdAt,omitempty"`

	// ExpiresAt is when the provider will delete the file (retention expiry,
	// e.g. from a requested upload TTL), if available from the provider.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// UploadSkillResult is returned by provider Skills API implementations.
type UploadSkillResult struct {
	ProviderReference ProviderReference      `json:"providerReference"`
	DisplayTitle      string                 `json:"displayTitle,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Description       string                 `json:"description,omitempty"`
	LatestVersion     string                 `json:"latestVersion,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
	Warnings          []Warning              `json:"warnings"`
}
