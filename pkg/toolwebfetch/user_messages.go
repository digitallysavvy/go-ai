package toolwebfetch

import (
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// urlPattern is the TS /https?:\/\/[^\s<>"'`]+/gi. JavaScript's \s is wider
// than RE2's: it also holds vertical tab, no-break space and the Unicode
// space separators, so text pasted from rich editors ends a URL there too.
var urlPattern = regexp.MustCompile("(?i)https?://[^\\s\\x0b\\x{a0}\\x{1680}\\x{2000}-\\x{200a}\\x{2028}\\x{2029}\\x{202f}\\x{205f}\\x{3000}\\x{feff}<>\"'`]+")

// extractURLs finds http(s) URLs in text. Punctuation at the end of a match,
// as in "see https://example.com/docs.", is also tried without it.
func extractURLs(text string) []string {
	var urls []string
	for _, match := range urlPattern.FindAllString(text, -1) {
		urls = append(urls, match)
		end := len(match)
		for end > 0 && strings.ContainsRune(".,;:!?)]}", rune(match[end-1])) {
			end--
		}
		if end < len(match) {
			urls = append(urls, match[:end])
		}
	}
	return urls
}

// isURLInUserMessages checks whether a URL appears in the text of a user
// message.
//
// Only user messages count. Assistant messages, tool calls and tool results
// are excluded because the model controls their content, directly or through
// tool inputs that are reflected in outputs. Otherwise a model could make a
// URL fetchable by first writing it, for example a URL that encodes private
// data in its query string.
func isURLInUserMessages(rawURL string, messages []types.Message) bool {
	requested, ok := canonicalURL(rawURL)
	if !ok {
		return false
	}
	want := requested.String()

	// Compare whole parsed URLs, not substrings: a substring check would let
	// `https://example.com` match `https://example.com.attacker.org/`.
	for _, message := range messages {
		if message.Role != types.RoleUser {
			continue
		}
		for _, part := range message.Content {
			text, ok := part.(types.TextContent)
			if !ok {
				continue
			}
			for _, candidate := range extractURLs(text.Text) {
				if canon, ok := canonicalURL(candidate); ok && canon.String() == want {
					return true
				}
			}
		}
	}
	return false
}
