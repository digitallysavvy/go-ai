package alibaba

// SupportedURLs lists the URLs the model accepts directly, without the SDK
// downloading them first (TS supportedUrls).
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^https?://.*$`},
	}
}
