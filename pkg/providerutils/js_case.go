package providerutils

import "github.com/digitallysavvy/go-ai/pkg/internal/jscase"

// JSToLower is JavaScript's String.prototype.toLowerCase. Use it where TS
// lowercases data (header values, media types, model output, user input):
// Go's strings.ToLower differs for U+0130 (JS "i̇"), a word-final
// capital sigma (JS final sigma) and characters Go's Unicode tables lack.
func JSToLower(s string) string { return jscase.ToLower(s) }

// JSToUpper is JavaScript's String.prototype.toUpperCase (U+00DF to "SS",
// ligatures and other SpecialCasing expansions, which strings.ToUpper keeps
// as one rune).
func JSToUpper(s string) string { return jscase.ToUpper(s) }
