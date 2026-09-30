package ai

import (
	"fmt"
	"math/rand"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// defaultIDAlphabet is the default alphabet used by CreateIDGenerator and
// GenerateID, matching TS provider-utils generate-id.ts.
const defaultIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// CreateIDGeneratorOptions configures CreateIDGenerator. All fields are
// optional.
type CreateIDGeneratorOptions struct {
	// Prefix is prepended (with Separator) to every generated ID.
	Prefix string

	// Separator sits between Prefix and the random part of the ID. Defaults
	// to "-". Only consulted when Prefix is set.
	Separator string

	// Size is the length of the random part of the ID. Defaults to 16.
	Size int

	// Alphabet is the character set used for the random part of the ID.
	// Defaults to "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz".
	Alphabet string
}

// CreateIDGenerator creates an ID generator. The total length of a generated
// ID is the sum of the prefix, separator, and random part length. Not
// cryptographically secure (uses math/rand, matching TS's Math.random()
// based implementation). This is a public utility mirroring TS
// provider-utils's createIdGenerator/generateId, also re-exported from the
// top-level `ai` package in TS -- used by callers to build custom ID
// generators (e.g. for chat message IDs).
//
// Returns a *provider/errors.InvalidArgumentError if Prefix is set and
// Separator appears inside Alphabet, which would make prefix detection
// ambiguous (matches TS's synchronous throw at construction time).
func CreateIDGenerator(opts ...CreateIDGeneratorOptions) (IDGenerator, error) {
	options := CreateIDGeneratorOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}

	size := options.Size
	if size == 0 {
		size = 16
	}
	alphabet := options.Alphabet
	if alphabet == "" {
		alphabet = defaultIDAlphabet
	}
	separator := options.Separator
	if separator == "" {
		separator = "-"
	}

	generator := func() string {
		alphabetLen := len(alphabet)
		chars := make([]byte, size)
		for i := 0; i < size; i++ {
			chars[i] = alphabet[rand.Intn(alphabetLen)]
		}
		return string(chars)
	}

	if options.Prefix == "" {
		return generator, nil
	}

	// The separator must not be part of the alphabet, otherwise prefix
	// checking can fail randomly.
	if strings.Contains(alphabet, separator) {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "separator",
			Message: fmt.Sprintf("The separator %q must not be part of the alphabet %q.", separator, alphabet),
		}
	}

	prefix := options.Prefix
	return func() string {
		return prefix + separator + generator()
	}, nil
}

// defaultIDGenerator backs GenerateID. Default options (no prefix) can never
// produce an error, so discarding it here is safe.
var defaultIDGenerator, _ = CreateIDGenerator()

// GenerateID generates a 16-character random string to use for IDs. Not
// cryptographically secure. Equivalent to calling CreateIDGenerator()().
// Mirrors TS provider-utils's exported `generateId` default instance.
func GenerateID() string {
	return defaultIDGenerator()
}
