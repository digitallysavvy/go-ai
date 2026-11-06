package types

// Citation is a reference supporting generated text. Citations are attached to
// the text they support, independently of the complete set of retrieved
// sources (TS LanguageModelV4Citation).
type Citation struct {
	// Source is the referenced URL or document.
	Source SourceContent `json:"source"`

	// StartIndex is the start offset in the containing generated text, when
	// supplied by the provider.
	StartIndex *int `json:"startIndex,omitempty"`

	// EndIndex is the exclusive end offset in the containing generated text,
	// when supplied by the provider.
	EndIndex *int `json:"endIndex,omitempty"`

	// CitedText is the supporting passage from the source, when supplied by the
	// provider.
	CitedText string `json:"citedText,omitempty"`
}
