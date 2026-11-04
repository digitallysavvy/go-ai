package quiverai

import "testing"

// TS isSvg regexes are /.../i without the u flag, so "svg" never matches
// "ſvg" (Go (?i) folds s to U+017F). Expected values from node v24 running
// the TS regexes (quiverai-image-model.ts isSvg head check;
// prepare-quiverai-image-reference.ts isSvg head + tail checks).
func TestR86btoolsSVGRegexesDoNotFoldLongS(t *testing.T) {
	for _, c := range []struct {
		in           string
		head, strict bool
	}{
		{"<ſvg></svg>", false, false},
		{"<SVG></SVG>", true, true},
		{"<svg></ſvg>", true, false},
		{"<ſvg/>", false, false},
		{"<svg />", true, true},
	} {
		if got := isSVGHead([]byte(c.in)); got != c.head {
			t.Errorf("isSVGHead(%q) = %v, want %v", c.in, got, c.head)
		}
		if got := isSVGBytes([]byte(c.in)); got != c.strict {
			t.Errorf("isSVGBytes(%q) = %v, want %v", c.in, got, c.strict)
		}
	}
}
