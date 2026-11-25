package providerutils

import "testing"

// TestR92integZodURLChecksTrimmedValue: zod 4 $ZodURL (z.url() and
// z.string().url()) checks payload.value.trim() with URL.canParse, so a URL
// with surrounding whitespace is valid; a value no WHATWG parser accepts is
// not.
func TestR92integZodURLChecksTrimmedValue(t *testing.T) {
	for _, ok := range []string{" https://x.dev/a\n", "https://x.dev/a", "mailto:a@b.c", "http:x.dev"} {
		if bad := zURLStr(ok, true, nil); bad != nil {
			t.Errorf("zURLStr(%q) = %v, want valid", ok, bad)
		}
	}
	for _, bad := range []string{"not a url", "", "  ", "//x.dev/a"} {
		if zURLStr(bad, true, nil) == nil {
			t.Errorf("zURLStr(%q) valid, want Invalid URL", bad)
		}
	}
}
