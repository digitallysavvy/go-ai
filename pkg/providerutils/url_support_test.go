package providerutils

import "testing"

// TestMatchesSupportedURL ports the relevant behavioral cases from TS
// is-url-supported.test.ts (packages/provider-utils/src/is-url-supported.test.ts).
// Go's regexp.Regexp has no analogue to JS's stateful global/sticky regexp
// lastIndex, so the "stateful regular expressions" describe block is not
// ported (nothing to regress there).
func TestMatchesSupportedURL(t *testing.T) {
	tests := []struct {
		name          string
		supportedUrls map[string][]string
		mediaType     string
		url           string
		want          bool
	}{
		{
			name:          "no supported URLs returns false",
			supportedUrls: map[string][]string{},
			mediaType:     "text/plain",
			url:           "https://example.com",
			want:          false,
		},
		// ai@7.0.118 commit bc49f786f0: exact-key matching must not fall back
		// to a prefix match (e.g. "image/png" must not match
		// "image/png-not-supported").
		{
			name:          "exact key does not prefix-match a longer media type (suffix text)",
			supportedUrls: map[string][]string{"image/png": {`^gs://`}},
			mediaType:     "image/png-not-supported",
			url:           "gs://example-bucket/image",
			want:          false,
		},
		{
			name:          "exact key does not prefix-match a longer media type (plus)",
			supportedUrls: map[string][]string{"image/png": {`^gs://`}},
			mediaType:     "image/png+invalid",
			url:           "gs://example-bucket/image",
			want:          false,
		},
		{
			name:          "exact key does not prefix-match a longer media type (semicolon)",
			supportedUrls: map[string][]string{"image/png": {`^gs://`}},
			mediaType:     "image/png;invalid",
			url:           "gs://example-bucket/image",
			want:          false,
		},
		{
			name:          "exact media type and exact URL match",
			supportedUrls: map[string][]string{"text/plain": {`https://example\.com`}},
			mediaType:     "text/plain",
			url:           "https://example.com",
			want:          true,
		},
		{
			name:          "exact media type and regex URL match",
			supportedUrls: map[string][]string{"image/png": {`https://images\.example\.com/.+`}},
			mediaType:     "image/png",
			url:           "https://images.example.com/cat.png",
			want:          true,
		},
		{
			name: "exact media type and one of multiple regex URLs match",
			supportedUrls: map[string][]string{"image/png": {
				`https://images\.example\.com/.+`,
				`https://another\.com/img\.png`,
			}},
			mediaType: "image/png",
			url:       "https://another.com/img.png",
			want:      true,
		},
		{
			name:          "exact media type but URL mismatch",
			supportedUrls: map[string][]string{"text/plain": {`https://example\.com`}},
			mediaType:     "text/plain",
			url:           "https://another.com",
			want:          false,
		},
		{
			name:          "URL match but media type mismatch",
			supportedUrls: map[string][]string{"text/plain": {`https://example\.com`}},
			mediaType:     "image/png",
			url:           "https://example.com",
			want:          false,
		},
		{
			name:          "wildcard media type and exact URL match",
			supportedUrls: map[string][]string{"*": {`https://example\.com`}},
			mediaType:     "text/plain",
			url:           "https://example.com",
			want:          true,
		},
		{
			name:          "wildcard media type and regex URL match",
			supportedUrls: map[string][]string{"*": {`https://images\.example\.com/.+`}},
			mediaType:     "image/jpeg",
			url:           "https://images.example.com/dog.jpg",
			want:          true,
		},
		{
			name:          "wildcard media type but URL mismatch",
			supportedUrls: map[string][]string{"*": {`https://example\.com`}},
			mediaType:     "video/mp4",
			url:           "https://another.com",
			want:          false,
		},
		{
			name: "specific media type wins alongside wildcard",
			supportedUrls: map[string][]string{
				"text/plain": {`https://text\.com`},
				"*":          {`https://any\.com`},
			},
			mediaType: "text/plain",
			url:       "https://text.com",
			want:      true,
		},
		{
			name: "wildcard still matches when specific media type also exists",
			supportedUrls: map[string][]string{
				"text/plain": {`https://text\.com`},
				"*":          {`https://any\.com`},
			},
			mediaType: "text/plain",
			url:       "https://any.com",
			want:      true,
		},
		{
			name: "wildcard matches a non-specified media type",
			supportedUrls: map[string][]string{
				"text/plain": {`https://text\.com`},
				"*":          {`https://any\.com`},
			},
			mediaType: "image/png",
			url:       "https://any.com",
			want:      true,
		},
		{
			name:          "empty URL matches a catch-all pattern",
			supportedUrls: map[string][]string{"text/plain": {`.*`}},
			mediaType:     "text/plain",
			url:           "",
			want:          true,
		},
		{
			name:          "empty URL does not match a non-empty pattern",
			supportedUrls: map[string][]string{"text/plain": {`https://.+`}},
			mediaType:     "text/plain",
			url:           "",
			want:          false,
		},
		{
			name:          "media type matching is case-insensitive",
			supportedUrls: map[string][]string{"text/plain": {`https://example\.com`}},
			mediaType:     "TEXT/PLAIN",
			url:           "https://example.com",
			want:          true,
		},
		{
			name:          "URL matching is case-insensitive",
			supportedUrls: map[string][]string{"text/plain": {`https://example\.com/path`}},
			mediaType:     "text/plain",
			url:           "https://EXAMPLE.com/PATH",
			want:          true,
		},
		{
			name:          "wildcard subtype match",
			supportedUrls: map[string][]string{"image/*": {`https://example\.com`}},
			mediaType:     "image/png",
			url:           "https://example.com",
			want:          true,
		},
		{
			name: "falls back to full wildcard when subtype wildcard URL does not match",
			supportedUrls: map[string][]string{
				"image/*": {`https://images\.com`},
				"*":       {`https://any\.com`},
			},
			mediaType: "image/png",
			url:       "https://any.com",
			want:      true,
		},
		{
			name:          "top-level-only media type matches a type/* key",
			supportedUrls: map[string][]string{"image/*": {`https://example\.com/.+`}},
			mediaType:     "image",
			url:           "https://example.com/cat.png",
			want:          true,
		},
		{
			name:          "top-level-only media type matches the wildcard * key",
			supportedUrls: map[string][]string{"*": {`https://example\.com`}},
			mediaType:     "image",
			url:           "https://example.com",
			want:          true,
		},
		{
			name:          "top-level-only media type does NOT match a specific type/subtype key",
			supportedUrls: map[string][]string{"image/png": {`https://example\.com/.+`}},
			mediaType:     "image",
			url:           "https://example.com/cat.png",
			want:          false,
		},
		{
			name:          "top-level-only media type does NOT match a different top-level type/* key",
			supportedUrls: map[string][]string{"audio/*": {`https://example\.com/.+`}},
			mediaType:     "image",
			url:           "https://example.com/audio.mp3",
			want:          false,
		},
		{
			name:          "empty URL pattern array for a media type never matches",
			supportedUrls: map[string][]string{"text/plain": {}},
			mediaType:     "text/plain",
			url:           "https://example.com",
			want:          false,
		},
		{
			name: "falls back to wildcard when specific media type has an empty pattern array",
			supportedUrls: map[string][]string{
				"text/plain": {},
				"*":          {`https://any\.com`},
			},
			mediaType: "text/plain",
			url:       "https://any.com",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled := CompileSupportedURLPatterns(tt.supportedUrls)
			if got := MatchesSupportedURL(compiled, tt.mediaType, tt.url); got != tt.want {
				t.Errorf("MatchesSupportedURL(%q, %q) = %v, want %v", tt.mediaType, tt.url, got, tt.want)
			}
		})
	}
}

// TestCompileSupportedURLPatterns_EmptyInput verifies nil/empty input
// compiles to nil, and MatchesSupportedURL against a nil table is always
// false (matching TS's empty supportedUrls map behavior).
func TestCompileSupportedURLPatterns_EmptyInput(t *testing.T) {
	if got := CompileSupportedURLPatterns(nil); got != nil {
		t.Errorf("CompileSupportedURLPatterns(nil) = %#v, want nil", got)
	}
	if got := CompileSupportedURLPatterns(map[string][]string{}); got != nil {
		t.Errorf("CompileSupportedURLPatterns({}) = %#v, want nil", got)
	}
	if MatchesSupportedURL(nil, "text/plain", "https://example.com") {
		t.Error("MatchesSupportedURL(nil, ...) = true, want false")
	}
}
