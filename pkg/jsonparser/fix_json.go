package jsonparser

import "github.com/gatewaytofredom/pjson"

// FixJSON repairs incomplete JSON by closing open strings, arrays, objects,
// and partial literals so that a half-finished document parses as valid JSON.
func FixJSON(input string) string {
	if input == "" {
		return ""
	}
	p := pjson.New(
		pjson.WithMaxDepth(512),
		pjson.WithCompleteLiterals(),
		pjson.WithInitialCapacity(len(input)+16),
	)
	if _, err := p.WriteString(input); err != nil {
		return ""
	}
	return string(p.CompleteJSON())
}
