package types

// UploadFileOptions are the provider-facing options for a single file upload.
type UploadFileOptions struct {
	Data            FileData               `json:"data"`
	MediaType       string                 `json:"mediaType,omitempty"`
	Filename        string                 `json:"filename,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
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
	ProviderReference map[string]string      `json:"providerReference"`
	MediaType         string                 `json:"mediaType,omitempty"`
	Filename          string                 `json:"filename,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
	Warnings          []Warning              `json:"warnings"`
}

// UploadSkillResult is returned by provider Skills API implementations.
type UploadSkillResult struct {
	ProviderReference map[string]string      `json:"providerReference"`
	DisplayTitle      string                 `json:"displayTitle,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Description       string                 `json:"description,omitempty"`
	LatestVersion     string                 `json:"latestVersion,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
	Warnings          []Warning              `json:"warnings"`
}
