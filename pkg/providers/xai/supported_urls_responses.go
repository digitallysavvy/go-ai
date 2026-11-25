package xai

// SupportedURLs lists the URLs the model accepts directly, without the SDK
// downloading them first (TS supportedUrls).
func (m *ResponsesLanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*":         {`^https?://.*$`},
		"application/pdf": {`^https?://.*$`},
		"text/*":          {`^https?://.*$`},
	}
}
