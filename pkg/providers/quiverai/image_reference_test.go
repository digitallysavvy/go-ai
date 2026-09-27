package quiverai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestIsSvgMarkupWellFormedNamedEntities mirrors TS's hasValidXmlReferences
// (quiverai-image-model.ts): a well-formed SVG document that uses named HTML
// entity references (e.g. "&nbsp;", "&copy;") must not be rejected as
// malformed just because encoding/xml's default entity set only covers
// amp/lt/gt/apos/quot.
func TestIsSvgMarkupWellFormedNamedEntities(t *testing.T) {
	tests := []struct {
		name string
		svg  string
		want bool
	}{
		{
			name: "standard XML entity",
			svg:  `<svg><text>a &amp; b</text></svg>`,
			want: true,
		},
		{
			name: "named HTML entity nbsp",
			svg:  `<svg><text>a&nbsp;b</text></svg>`,
			want: true,
		},
		{
			name: "named HTML entity copy",
			svg:  `<svg><text>&copy; 2026</text></svg>`,
			want: true,
		},
		{
			name: "decimal numeric character reference",
			svg:  `<svg><text>&#65;</text></svg>`,
			want: true,
		},
		{
			name: "hex numeric character reference",
			svg:  `<svg><text>&#x41;</text></svg>`,
			want: true,
		},
		{
			name: "unterminated entity reference is malformed",
			svg:  `<svg><text>a &nbsp b</text></svg>`,
			want: false,
		},
		{
			name: "mismatched tags are still rejected",
			svg:  `<svg><g></svg>`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSvgMarkupWellFormed(tt.svg)
			if got != tt.want {
				t.Errorf("isSvgMarkupWellFormed(%q) = %v, want %v", tt.svg, got, tt.want)
			}
		})
	}
}

// TestToQuiverAIEditSourceAcceptsNamedEntities exercises the fix end-to-end
// through the public entry point used by the SVG edit request path.
func TestToQuiverAIEditSourceAcceptsNamedEntities(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><text>Caf&eacute; &amp; Cr&egrave;me</text></svg>`
	file := provider.ImageFile{Type: "file", Data: []byte(svg), MediaType: "image/svg+xml"}
	if _, err := toQuiverAIEditSource(file); err != nil {
		t.Fatalf("toQuiverAIEditSource() error = %v, want nil for a well-formed SVG with named entities", err)
	}
}
