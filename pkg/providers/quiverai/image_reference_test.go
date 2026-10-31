package quiverai

import (
	"strings"
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

func TestValidateQuiverAIImageURLMatchesWHATWGHref(t *testing.T) {
	got, err := validateQuiverAIImageURL("HTTPS://Example.COM:443", "providerOptions.quiverai.referenceImages[0]")
	if err != nil || got != "https://example.com/" {
		t.Fatalf("got %q, %v", got, err)
	}
	_, err = validateQuiverAIImageURL("ftp://example.com/a", "x")
	if err == nil || !strings.Contains(err.Error(), "url") || !strings.Contains(err.Error(), "must use HTTP or HTTPS") {
		t.Fatalf("want url-argument error, got %v", err)
	}
}

func TestToQuiverAIAnimationSourceURLKeepsOriginalAndMessages(t *testing.T) {
	src, err := toQuiverAIAnimationSource(provider.ImageFile{Type: "url", URL: "https://Example.com"})
	if err != nil || src["url"] != "https://Example.com" {
		t.Fatalf("got %v, %v", src, err)
	}
	_, err = toQuiverAIAnimationSource(provider.ImageFile{Type: "url", URL: "ftp://example.com/a.svg"})
	if err == nil || !strings.Contains(err.Error(), "requires an HTTP or HTTPS SVG URL.") {
		t.Fatalf("got %v", err)
	}
	_, err = toQuiverAIAnimationSource(provider.ImageFile{Type: "url", URL: "not a url"})
	if err == nil || !strings.Contains(err.Error(), "requires a valid HTTP or HTTPS SVG URL.") {
		t.Fatalf("got %v", err)
	}
}

func TestIsSVGBytesStrictHead(t *testing.T) {
	if !isSVGBytes([]byte(`<?xml version="1.0"?><!-- c --><!DOCTYPE svg><svg></svg>`)) {
		t.Fatal("xml+comment+doctype must be accepted")
	}
	if isSVGBytes([]byte(`<html></html>`)) {
		t.Fatal("non-svg must be rejected")
	}
}

// isSvgMarkupWellFormed is a port of the TS hand-written scanner, so it must
// agree with it on input a real XML parser would judge differently. Expected
// values are the TS isSvgMarkup results for these documents.
func TestIsSvgMarkupWellFormedMatchesTSScanner(t *testing.T) {
	tests := []struct {
		name string
		svg  string
		want bool
	}{
		{"any XML Name is an accepted entity", `<svg>&unknownEntity;</svg>`, true},
		{"duplicate attribute", `<svg a="1" a="2"></svg>`, false},
		{"attributes need separating whitespace", `<svg a="1"b="2"></svg>`, false},
		{"text outside the root", `x<svg></svg>`, false},
		{"second root", `<svg></svg><svg></svg>`, false},
		{"cdata outside any element", `<![CDATA[x]]><svg></svg>`, false},
		{"double hyphen in comment", `<!-- a -- b --><svg></svg>`, false},
		{"doctype for another root", `<!DOCTYPE html><svg></svg>`, false},
		{"doctype with internal subset", `<!DOCTYPE svg [<!ENTITY a "b">]><svg>&a;</svg>`, true},
		{"unquoted attribute", `<svg a=1></svg>`, false},
		{"lt inside attribute value", `<svg a="<"></svg>`, false},
		{"non-BMP name character", "<svg><😀/></svg>", true},
	}
	for _, tt := range tests {
		if got := isSvgMarkupWellFormed(tt.svg); got != tt.want {
			t.Errorf("%s: isSvgMarkupWellFormed(%q) = %v, want %v", tt.name, tt.svg, got, tt.want)
		}
	}
}
