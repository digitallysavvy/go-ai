package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
)

// Streaming extraction of large base64 strings from a JSON response body, a
// port of TS provider-utils json-binary-field-scanner.ts (89aff56165).
//
// A JSON response that carries an image is mostly one very long base64
// string. The scanner reads the body chunk by chunk: strings at the configured
// paths are decoded to bytes while they stream in, and the text that is
// parsed afterwards (the "skeleton") holds a short placeholder in their place.
//
//   - The skeleton is the original body except for the extracted strings, so
//     the JSON parser still sees every key (a base64 string cannot contain a
//     quote, so it cannot hide one).
//   - A string is extracted only if it is canonical standard-alphabet base64
//     (length divisible by 4, `=` only as the final padding, zero trailing
//     bits, no escape sequences or whitespace) and decodes to at least
//     binaryFieldMinBytes. Everything else stays a string: what was decoded
//     before a string turned out not to qualify is encoded back, which gives
//     the original characters.

// binaryFieldMinBytes is TS DEFAULT_BINARY_FIELD_MIN_BYTES: strings that
// decode to fewer bytes stay strings.
const binaryFieldMinBytes = 64 * 1024

// binaryFieldPaths is a node of the compiled path trie (TS BinaryFieldPaths):
// the edges are the bytes of an object key (a key ends with its closing
// quote), items for the elements of an array, and end for "a path ends here".
type binaryFieldPaths struct {
	keys  map[byte]*binaryFieldPaths
	items *binaryFieldPaths
	end   bool
}

var binaryFieldPathPattern = regexp.MustCompile(`^(?:[\w-]+|\[\*])(?:\.[\w-]+|\[\*])*$`)
var binaryFieldSegmentPattern = regexp.MustCompile(`\[\*]|[\w-]+`)

// compileBinaryFieldPaths compiles paths such as `images[*]` or
// `content[*].data.data` (TS compileBinaryFieldPaths). Keys are compared with
// the raw bytes of the key in the body, so a key written with an escape
// sequence never matches.
func compileBinaryFieldPaths(paths []string) (*binaryFieldPaths, error) {
	root := &binaryFieldPaths{}
	for _, path := range paths {
		if !binaryFieldPathPattern.MatchString(path) {
			return nil, fmt.Errorf("Invalid binary field path %q", path) //nolint:staticcheck // ST1005: TS InvalidArgumentError text
		}
		node := root
		for _, segment := range binaryFieldSegmentPattern.FindAllString(path, -1) {
			if segment == "[*]" {
				if node.items == nil {
					node.items = &binaryFieldPaths{}
				}
				node = node.items
				continue
			}
			for _, b := range []byte(segment + `"`) {
				if node.keys == nil {
					node.keys = map[byte]*binaryFieldPaths{}
				}
				next := node.keys[b]
				if next == nil {
					next = &binaryFieldPaths{}
					node.keys[b] = next
				}
				node = next
			}
		}
		node.end = true
	}
	return root, nil
}

func (n *binaryFieldPaths) key(b byte) *binaryFieldPaths {
	if n == nil {
		return nil
	}
	return n.keys[b]
}

// Scanner modes (TS OUTSIDE / KEY / SKIP / BLOB).
const (
	scanOutside = iota // between tokens
	scanKey            // in an object key
	scanSkip           // in a string value that stays as it is
	scanBlob           // in a string value that is decoded while it streams in
)

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// base64Sextet is the six-bit value of each base64 character, 64 for
// everything else.
var base64Sextet = func() (table [256]byte) {
	for i := range table {
		table[i] = 64
	}
	for i := 0; i < 64; i++ {
		table[base64Alphabet[i]] = byte(i)
	}
	return table
}()

type binaryFieldFrame struct {
	keys *binaryFieldPaths // the node whose edges match the next key; nil in an array
	next *binaryFieldPaths // the node of the value that is read next
}

// jsonBinaryFieldScanner is TS createJsonBinaryFieldScanner.
type jsonBinaryFieldScanner struct {
	paths    *binaryFieldPaths
	minBytes int

	skeleton bytes.Buffer
	binaries map[string][]byte

	// JSON structure. Only the containers on a path get a frame; the ones
	// that are not are counted in skipped.
	frames  []binaryFieldFrame
	skipped int
	mode    int
	escaped bool
	last    byte // the last `{`, `,` or `:` outside of strings
	keyNode *binaryFieldPaths

	// The candidate string: whole groups of four characters are decoded into
	// decoded; the characters of an unfinished group are kept in bits.
	decoded []byte
	bits    uint32
	chars   int // characters in bits
	padding int // `=` characters seen
}

func newJSONBinaryFieldScanner(paths *binaryFieldPaths, minBytes int) *jsonBinaryFieldScanner {
	return &jsonBinaryFieldScanner{paths: paths, minBytes: minBytes, binaries: map[string][]byte{}}
}

func (s *jsonBinaryFieldScanner) reset() {
	s.decoded = nil
	s.bits = 0
	s.chars = 0
	s.padding = 0
}

// rest is the unfinished group, as the bytes of a group of three padded with
// zeros.
func (s *jsonBinaryFieldScanner) rest() uint32 {
	return s.bits << ((4 - s.chars) * 6)
}

// restore puts the characters of a string that was not extracted back into
// the skeleton.
func (s *jsonBinaryFieldScanner) restore() {
	if len(s.decoded) > 0 {
		s.skeleton.WriteString(base64.StdEncoding.EncodeToString(s.decoded))
	}
	group := s.rest()
	tail := base64.StdEncoding.EncodeToString([]byte{byte(group >> 16), byte(group >> 8), byte(group)})
	s.skeleton.WriteString(tail[:s.chars])
	s.skeleton.WriteString(strings.Repeat("=", s.padding))
	s.reset()
}

// closeBlob handles the closing quote of a candidate string.
func (s *jsonBinaryFieldScanner) closeBlob() {
	s.mode = scanOutside
	tail := 0
	if s.chars > 0 {
		tail = s.chars - 1
	}
	if s.padding == (4-s.chars)%4 &&
		s.bits&((1<<((4-s.chars)*2))-1) == 0 &&
		len(s.decoded)+tail >= s.minBytes {
		group := s.rest()
		data := s.decoded
		for i := 0; i < tail; i++ {
			data = append(data, byte(group>>(16-8*i)))
		}
		placeholder := fmt.Sprintf("[binary data: %d bytes; ref %s]", len(data), binaryFieldRef())
		s.binaries[placeholder] = data
		s.skeleton.WriteString(placeholder)
		s.reset()
		return
	}
	s.restore()
}

// binaryFieldRef is TS crypto.getRandomValues(new Uint32Array(2)).join('-').
func binaryFieldRef() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%d", binary.LittleEndian.Uint32(b[:4]), binary.LittleEndian.Uint32(b[4:]))
}

func (s *jsonBinaryFieldScanner) frame() *binaryFieldFrame {
	if len(s.frames) == 0 {
		return nil
	}
	return &s.frames[len(s.frames)-1]
}

// push scans one chunk of the body.
func (s *jsonBinaryFieldScanner) push(chunk []byte) {
	from := 0 // the first byte of the chunk that is not in the skeleton yet
	length := len(chunk)

	for index := 0; index < length; index++ {
		b := chunk[index]

		if s.mode == scanBlob {
			if s.padding == 0 {
				pending := s.bits
				count := s.chars
				for index < length {
					value := base64Sextet[chunk[index]]
					if value >= 64 {
						break
					}
					pending = pending<<6 | uint32(value)
					count++
					if count == 4 {
						s.decoded = append(s.decoded, byte(pending>>16), byte(pending>>8), byte(pending))
						pending = 0
						count = 0
					}
					index++
				}
				s.bits = pending
				s.chars = count
			}
			if index >= length {
				break
			}
			b = chunk[index]
			switch {
			case b == '"':
				s.closeBlob()
				from = index
			case b == '=' && s.chars > 1 && s.padding < 4-s.chars:
				// Padding completes a group of two or three characters.
				s.padding++
			default:
				// Not canonical. The string goes on as a regular string,
				// from this byte.
				s.restore()
				s.mode = scanSkip
				from = index
				index--
			}
			continue
		}

		if s.mode != scanOutside {
			if s.mode == scanKey {
				s.keyNode = s.keyNode.key(b)
			}
			switch {
			case s.escaped:
				s.escaped = false
			case b == '\\':
				s.escaped = true
			case b == '"':
				if s.mode == scanKey {
					if f := s.frame(); f != nil {
						f.next = s.keyNode
					}
				}
				s.mode = scanOutside
			}
			continue
		}

		switch b {
		case '"':
			f := s.frame()
			var valueNode *binaryFieldPaths
			if f != nil {
				valueNode = f.next
			} else {
				valueNode = s.paths
			}
			switch {
			case s.skipped > 0:
				s.mode = scanSkip
			case f != nil && f.keys != nil && (s.last == '{' || s.last == ','):
				s.mode = scanKey
				s.keyNode = f.keys
			case valueNode != nil && valueNode.end:
				s.mode = scanBlob
				// The opening quote still belongs to the skeleton.
				s.skeleton.Write(chunk[from : index+1])
			default:
				s.mode = scanSkip
			}
		case '{', '[':
			s.last = b
			var node *binaryFieldPaths
			if s.skipped == 0 {
				if f := s.frame(); f != nil {
					node = f.next
				} else {
					node = s.paths
				}
			}
			switch {
			case b == '{' && node != nil:
				s.frames = append(s.frames, binaryFieldFrame{keys: node})
			case b == '[' && node != nil && node.items != nil:
				s.frames = append(s.frames, binaryFieldFrame{next: node.items})
			default:
				s.skipped++
			}
		case '}', ']':
			if s.skipped > 0 {
				s.skipped--
			} else if len(s.frames) > 0 {
				s.frames = s.frames[:len(s.frames)-1]
			}
		case ',', ':':
			s.last = b
		}
	}

	if s.mode != scanBlob {
		s.skeleton.Write(chunk[from:length])
	}
}

// finish returns the skeleton, decoded like TS new TextDecoder().decode, and
// the extracted bytes by placeholder. A body that ends inside a candidate
// string is returned as it was received, so that the parser reports the
// truncation.
func (s *jsonBinaryFieldScanner) finish() (string, map[string][]byte) {
	if s.mode == scanBlob {
		s.restore()
	}
	return textdecode.UTF8(s.skeleton.Bytes()), s.binaries
}
