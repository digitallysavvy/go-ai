// Package urlnorm parses absolute URLs with the WHATWG URL Standard's basic
// URL parser (no base URL) and serializes them the way JavaScript's
// `new URL(input).href` does.
//
// The TypeScript AI SDK turns every URL file part into a `URL` object and
// sends `url.toString()` to providers, so a Go port that forwards the
// caller's string as given differs on the wire whenever the string is not
// already in WHATWG serialized form (scheme or host case, default ports, dot
// segments, spaces and non-ASCII characters, IDNA hosts, backslashes, ...).
// Go's net/url implements RFC 3986 and does none of that, hence this package.
//
// The parser follows https://url.spec.whatwg.org/#concept-basic-url-parser
// for the no-base, no-state-override case, with UTF-8 query encoding. It is
// differential-tested against Node's `new URL(x).href` (see the testdata
// corpus and its generator).
package urlnorm

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrInvalidURL is returned for any input the WHATWG basic URL parser
// rejects. Its text is the message of the TypeError `new URL(x)` throws.
var ErrInvalidURL = errors.New("Invalid URL") //nolint:staticcheck // TS/Node TypeError text

// URL is a parsed WHATWG URL record.
type URL struct {
	// Scheme is the lowercased scheme without the trailing ':'.
	Scheme   string
	Username string
	Password string
	// Host is the serialized host. HasHost distinguishes a null host from
	// the empty host (as in "file:///x" or "foo://").
	Host    string
	HasHost bool
	// Port is the port; -1 when null (absent or the scheme's default).
	Port int
	// path holds the path segments; opaquePath is used instead when the URL
	// has an opaque path (e.g. "mailto:x", "data:...").
	path       []string
	opaquePath string
	opaque     bool
	Query      *string
	Fragment   *string
}

var specialSchemes = map[string]int{
	"ftp":   21,
	"file":  -1,
	"http":  80,
	"https": 443,
	"ws":    80,
	"wss":   443,
}

func isSpecialScheme(scheme string) bool {
	_, ok := specialSchemes[scheme]
	return ok
}

// IsSpecial reports whether the URL's scheme is a WHATWG special scheme.
func (u *URL) IsSpecial() bool { return isSpecialScheme(u.Scheme) }

// Href parses input and returns its serialization (`new URL(input).href`).
func Href(input string) (string, error) {
	u, err := Parse(input)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Pathname returns the URL's path serialization (`URL.prototype.pathname`).
func (u *URL) Pathname() string {
	if u.opaque {
		return u.opaquePath
	}
	var b strings.Builder
	for _, segment := range u.path {
		b.WriteByte('/')
		b.WriteString(segment)
	}
	return b.String()
}

// String returns the URL serializer output (`URL.prototype.href`).
func (u *URL) String() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteByte(':')
	if u.HasHost {
		b.WriteString("//")
		if u.Username != "" || u.Password != "" {
			b.WriteString(u.Username)
			if u.Password != "" {
				b.WriteByte(':')
				b.WriteString(u.Password)
			}
			b.WriteByte('@')
		}
		b.WriteString(u.Host)
		if u.Port >= 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(u.Port))
		}
	} else if !u.opaque && len(u.path) > 1 && u.path[0] == "" {
		b.WriteString("/.")
	}
	b.WriteString(u.Pathname())
	if u.Query != nil {
		b.WriteByte('?')
		b.WriteString(*u.Query)
	}
	if u.Fragment != nil {
		b.WriteByte('#')
		b.WriteString(*u.Fragment)
	}
	return b.String()
}

type parserState int

const (
	stateSchemeStart parserState = iota
	stateScheme
	stateSpecialAuthoritySlashes
	stateSpecialAuthorityIgnoreSlashes
	statePathOrAuthority
	stateAuthority
	stateHost
	statePort
	stateFile
	stateFileSlash
	stateFileHost
	statePathStart
	statePath
	stateOpaquePath
	stateQuery
	stateFragment
)

const eof rune = -1

// Parse runs the WHATWG basic URL parser on input without a base URL.
func Parse(input string) (*URL, error) {
	input = trimC0ControlOrSpace(input)
	runes := make([]rune, 0, len(input))
	for _, r := range input { // invalid UTF-8 bytes decode to U+FFFD
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		runes = append(runes, r)
	}

	u := &URL{Port: -1}
	state := stateSchemeStart
	var buffer strings.Builder
	atSignSeen, insideBrackets, passwordTokenSeen := false, false, false

	at := func(i int) rune {
		if i >= 0 && i < len(runes) {
			return runes[i]
		}
		return eof
	}
	remainingStartsWith := func(p int, s string) bool {
		i := p + 1
		for _, r := range s {
			if at(i) != r {
				return false
			}
			i++
		}
		return true
	}

	for p := 0; ; p++ {
		c := at(p)
		switch state {
		case stateSchemeStart:
			if isASCIIAlpha(c) {
				buffer.WriteRune(toLowerASCII(c))
				state = stateScheme
			} else {
				return nil, ErrInvalidURL // no scheme and no base URL
			}

		case stateScheme:
			switch {
			case isASCIIAlphanumeric(c) || c == '+' || c == '-' || c == '.':
				buffer.WriteRune(toLowerASCII(c))
			case c == ':':
				u.Scheme = buffer.String()
				buffer.Reset()
				switch {
				case u.Scheme == "file":
					state = stateFile
				case u.IsSpecial():
					state = stateSpecialAuthoritySlashes
				case remainingStartsWith(p, "/"):
					state = statePathOrAuthority
					p++
				default:
					u.opaque = true
					state = stateOpaquePath
				}
			default:
				return nil, ErrInvalidURL
			}

		case stateSpecialAuthoritySlashes:
			if c == '/' && remainingStartsWith(p, "/") {
				state = stateSpecialAuthorityIgnoreSlashes
				p++
			} else {
				state = stateSpecialAuthorityIgnoreSlashes
				p--
			}

		case stateSpecialAuthorityIgnoreSlashes:
			if c != '/' && c != '\\' {
				state = stateAuthority
				p--
			}

		case statePathOrAuthority:
			if c == '/' {
				state = stateAuthority
			} else {
				state = statePath
				p--
			}

		case stateAuthority:
			switch {
			case c == '@':
				if atSignSeen {
					buf := buffer.String()
					buffer.Reset()
					buffer.WriteString("%40")
					buffer.WriteString(buf)
				}
				atSignSeen = true
				for _, cp := range buffer.String() {
					if cp == ':' && !passwordTokenSeen {
						passwordTokenSeen = true
						continue
					}
					encoded := percentEncodeRune(cp, inUserinfoSet)
					if passwordTokenSeen {
						u.Password += encoded
					} else {
						u.Username += encoded
					}
				}
				buffer.Reset()
			case c == eof || c == '/' || c == '?' || c == '#' || (u.IsSpecial() && c == '\\'):
				if atSignSeen && buffer.Len() == 0 {
					return nil, ErrInvalidURL
				}
				p -= utf8.RuneCountInString(buffer.String()) + 1
				buffer.Reset()
				state = stateHost
			default:
				buffer.WriteRune(c)
			}

		case stateHost:
			switch {
			case c == ':' && !insideBrackets:
				if buffer.Len() == 0 {
					return nil, ErrInvalidURL
				}
				host, err := parseHost(buffer.String(), !u.IsSpecial())
				if err != nil {
					return nil, err
				}
				u.Host, u.HasHost = host, true
				buffer.Reset()
				state = statePort
			case c == eof || c == '/' || c == '?' || c == '#' || (u.IsSpecial() && c == '\\'):
				p--
				if u.IsSpecial() && buffer.Len() == 0 {
					return nil, ErrInvalidURL
				}
				host, err := parseHost(buffer.String(), !u.IsSpecial())
				if err != nil {
					return nil, err
				}
				u.Host, u.HasHost = host, true
				buffer.Reset()
				state = statePathStart
			default:
				if c == '[' {
					insideBrackets = true
				}
				if c == ']' {
					insideBrackets = false
				}
				buffer.WriteRune(c)
			}

		case statePort:
			switch {
			case isASCIIDigit(c):
				buffer.WriteRune(c)
			case c == eof || c == '/' || c == '?' || c == '#' || (u.IsSpecial() && c == '\\'):
				if buffer.Len() != 0 {
					port := 0
					for _, d := range buffer.String() {
						port = port*10 + int(d-'0')
						if port > 65535 {
							return nil, ErrInvalidURL
						}
					}
					if def, ok := specialSchemes[u.Scheme]; ok && def == port {
						u.Port = -1
					} else {
						u.Port = port
					}
					buffer.Reset()
				}
				state = statePathStart
				p--
			default:
				return nil, ErrInvalidURL
			}

		case stateFile:
			u.Scheme = "file"
			u.Host, u.HasHost = "", true
			if c == '/' || c == '\\' {
				state = stateFileSlash
			} else {
				state = statePath
				p--
			}

		case stateFileSlash:
			if c == '/' || c == '\\' {
				state = stateFileHost
			} else {
				state = statePath
				p--
			}

		case stateFileHost:
			if c == eof || c == '/' || c == '\\' || c == '?' || c == '#' {
				p--
				switch {
				case isWindowsDriveLetter(buffer.String()):
					// The buffer is kept and becomes the first path segment.
					state = statePath
				case buffer.Len() == 0:
					u.Host, u.HasHost = "", true
					state = statePathStart
				default:
					host, err := parseHost(buffer.String(), false)
					if err != nil {
						return nil, err
					}
					if host == "localhost" {
						host = ""
					}
					u.Host, u.HasHost = host, true
					buffer.Reset()
					state = statePathStart
				}
			} else {
				buffer.WriteRune(c)
			}

		case statePathStart:
			switch {
			case u.IsSpecial():
				state = statePath
				if c != '/' && c != '\\' {
					p--
				}
			case c == '?':
				u.Query = new(string)
				state = stateQuery
			case c == '#':
				u.Fragment = new(string)
				state = stateFragment
			case c != eof:
				state = statePath
				if c != '/' {
					p--
				}
			}

		case statePath:
			if c == eof || c == '/' || (u.IsSpecial() && c == '\\') || c == '?' || c == '#' {
				slash := c == '/' || (u.IsSpecial() && c == '\\')
				seg := buffer.String()
				switch {
				case isDoubleDotSegment(seg):
					u.shortenPath()
					if !slash {
						u.path = append(u.path, "")
					}
				case isSingleDotSegment(seg):
					if !slash {
						u.path = append(u.path, "")
					}
				default:
					if u.Scheme == "file" && len(u.path) == 0 && isWindowsDriveLetter(seg) {
						seg = seg[:1] + ":" + seg[2:]
					}
					u.path = append(u.path, seg)
				}
				buffer.Reset()
				if c == '?' {
					u.Query = new(string)
					state = stateQuery
				}
				if c == '#' {
					u.Fragment = new(string)
					state = stateFragment
				}
			} else {
				buffer.WriteString(percentEncodeRune(c, inPathSet))
			}

		case stateOpaquePath:
			switch {
			case c == '?':
				u.Query = new(string)
				state = stateQuery
			case c == '#':
				u.Fragment = new(string)
				state = stateFragment
			case c == ' ':
				if next := at(p + 1); next == '?' || next == '#' {
					u.opaquePath += "%20"
				} else {
					u.opaquePath += " "
				}
			case c != eof:
				u.opaquePath += percentEncodeRune(c, inC0ControlSet)
			}

		case stateQuery:
			if c == '#' || c == eof {
				set := inQuerySet
				if u.IsSpecial() {
					set = inSpecialQuerySet
				}
				*u.Query += percentEncodeString(buffer.String(), set)
				buffer.Reset()
				if c == '#' {
					u.Fragment = new(string)
					state = stateFragment
				}
			} else {
				buffer.WriteRune(c)
			}

		case stateFragment:
			if c != eof {
				*u.Fragment += percentEncodeRune(c, inFragmentSet)
			}
		}

		// Spec: stop once the pointer (after any decrement) is at EOF.
		if p >= len(runes) {
			break
		}
	}
	return u, nil
}

func (u *URL) shortenPath() {
	if u.Scheme == "file" && len(u.path) == 1 && isNormalizedWindowsDriveLetter(u.path[0]) {
		return
	}
	if len(u.path) > 0 {
		u.path = u.path[:len(u.path)-1]
	}
}

func trimC0ControlOrSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r >= 0 && r <= 0x20 })
}

func isASCIIAlpha(c rune) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isASCIIDigit(c rune) bool { return c >= '0' && c <= '9' }
func isASCIIAlphanumeric(c rune) bool {
	return isASCIIAlpha(c) || isASCIIDigit(c)
}
func isASCIIHexDigit(c rune) bool {
	return isASCIIDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func toLowerASCII(c rune) rune {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func isWindowsDriveLetter(s string) bool {
	return len(s) == 2 && isASCIIAlpha(rune(s[0])) && (s[1] == ':' || s[1] == '|')
}

func isNormalizedWindowsDriveLetter(s string) bool {
	return len(s) == 2 && isASCIIAlpha(rune(s[0])) && s[1] == ':'
}

func isSingleDotSegment(s string) bool {
	return s == "." || strings.EqualFold(s, "%2e")
}

func isDoubleDotSegment(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// Percent-encode sets (https://url.spec.whatwg.org/#percent-encoded-bytes).
// Each reports whether an ASCII code point must be encoded; every non-ASCII
// code point is always encoded.

func inC0ControlSet(c rune) bool { return c <= 0x1f || c > 0x7e }

func inFragmentSet(c rune) bool {
	return inC0ControlSet(c) || c == ' ' || c == '"' || c == '<' || c == '>' || c == '`'
}

func inQuerySet(c rune) bool {
	return inC0ControlSet(c) || c == ' ' || c == '"' || c == '#' || c == '<' || c == '>'
}

func inSpecialQuerySet(c rune) bool { return inQuerySet(c) || c == '\'' }

func inPathSet(c rune) bool {
	return inQuerySet(c) || c == '?' || c == '^' || c == '`' || c == '{' || c == '}'
}

func inUserinfoSet(c rune) bool {
	return inPathSet(c) || c == '/' || c == ':' || c == ';' || c == '=' || c == '@' ||
		(c >= '[' && c <= ']') || c == '|'
}

const upperHex = "0123456789ABCDEF"

func percentEncodeRune(c rune, inSet func(rune) bool) string {
	if !inSet(c) {
		return string(c)
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], c)
	var b strings.Builder
	for _, x := range buf[:n] {
		b.WriteByte('%')
		b.WriteByte(upperHex[x>>4])
		b.WriteByte(upperHex[x&0xf])
	}
	return b.String()
}

func percentEncodeString(s string, inSet func(rune) bool) string {
	var b strings.Builder
	for _, c := range s {
		b.WriteString(percentEncodeRune(c, inSet))
	}
	return b.String()
}

func percentDecode(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isASCIIHexDigit(rune(s[i+1])) && isASCIIHexDigit(rune(s[i+2])) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			out = append(out, byte(v))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return out
}

func isForbiddenHostCodePoint(c rune) bool {
	switch c {
	case 0, '\t', '\n', '\r', ' ', '#', '/', ':', '<', '>', '?', '@', '[', '\\', ']', '^', '|':
		return true
	}
	return false
}

func isForbiddenDomainCodePoint(c rune) bool {
	return isForbiddenHostCodePoint(c) || (c >= 0 && c <= 0x1f) || c == '%' || c == 0x7f
}

// parseHost is the WHATWG host parser.
func parseHost(input string, isOpaque bool) (string, error) {
	if strings.HasPrefix(input, "[") {
		if !strings.HasSuffix(input, "]") || len(input) < 2 {
			return "", ErrInvalidURL
		}
		pieces, ok := parseIPv6([]rune(input[1 : len(input)-1]))
		if !ok {
			return "", ErrInvalidURL
		}
		return "[" + serializeIPv6(pieces) + "]", nil
	}
	if isOpaque {
		for _, c := range input {
			if isForbiddenHostCodePoint(c) {
				return "", ErrInvalidURL
			}
		}
		return percentEncodeString(input, inC0ControlSet), nil
	}
	domain := strings.ToValidUTF8(string(percentDecode(input)), "�")
	ascii, err := domainToASCII(domain)
	if err != nil {
		return "", err
	}
	if endsInANumber(ascii) {
		return parseIPv4(ascii)
	}
	return ascii, nil
}

func endsInANumber(input string) bool {
	parts := strings.Split(input, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok
}

// parseIPv4Number returns the value (saturated above 2^32) and whether the
// part is a valid IPv4 number.
func parseIPv4Number(input string) (uint64, bool) {
	if input == "" {
		return 0, false
	}
	radix := uint64(10)
	switch {
	case len(input) >= 2 && (input[:2] == "0x" || input[:2] == "0X"):
		radix = 16
		input = input[2:]
	case len(input) >= 2 && input[0] == '0':
		radix = 8
		input = input[1:]
	}
	if input == "" {
		return 0, true
	}
	var value uint64
	for i := 0; i < len(input); i++ {
		d, ok := digitValue(input[i])
		if !ok || d >= radix {
			return 0, false
		}
		if value <= 1<<33 {
			value = value*radix + d
		}
	}
	return value, true
}

func digitValue(c byte) (uint64, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint64(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

func parseIPv4(input string) (string, error) {
	parts := strings.Split(input, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", ErrInvalidURL
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := parseIPv4Number(part)
		if !ok {
			return "", ErrInvalidURL
		}
		numbers[i] = n
	}
	for _, n := range numbers[:len(numbers)-1] {
		if n > 255 {
			return "", ErrInvalidURL
		}
	}
	last := numbers[len(numbers)-1]
	if last >= uint64(1)<<(8*(5-len(numbers))) {
		return "", ErrInvalidURL
	}
	ipv4 := last
	for i, n := range numbers[:len(numbers)-1] {
		ipv4 += n << (8 * (3 - i))
	}
	return strconv.FormatUint(ipv4>>24, 10) + "." +
		strconv.FormatUint(ipv4>>16&0xff, 10) + "." +
		strconv.FormatUint(ipv4>>8&0xff, 10) + "." +
		strconv.FormatUint(ipv4&0xff, 10), nil
}

func parseIPv6(input []rune) ([8]uint16, bool) {
	var address [8]uint16
	pieceIndex, compress := 0, -1
	p := 0
	at := func(i int) rune {
		if i < len(input) {
			return input[i]
		}
		return eof
	}
	if at(p) == ':' {
		if at(p+1) != ':' {
			return address, false
		}
		p += 2
		pieceIndex++
		compress = pieceIndex
	}
	for at(p) != eof {
		if pieceIndex == 8 {
			return address, false
		}
		if at(p) == ':' {
			if compress != -1 {
				return address, false
			}
			p++
			pieceIndex++
			compress = pieceIndex
			continue
		}
		value, length := 0, 0
		for length < 4 && isASCIIHexDigit(at(p)) {
			d, _ := digitValue(byte(at(p)))
			value = value*0x10 + int(d)
			p++
			length++
		}
		if at(p) == '.' {
			if length == 0 {
				return address, false
			}
			p -= length
			if pieceIndex > 6 {
				return address, false
			}
			numbersSeen := 0
			for at(p) != eof {
				ipv4Piece := -1
				if numbersSeen > 0 {
					if at(p) == '.' && numbersSeen < 4 {
						p++
					} else {
						return address, false
					}
				}
				if !isASCIIDigit(at(p)) {
					return address, false
				}
				for isASCIIDigit(at(p)) {
					n := int(at(p) - '0')
					switch ipv4Piece {
					case -1:
						ipv4Piece = n
					case 0:
						return address, false
					default:
						ipv4Piece = ipv4Piece*10 + n
					}
					if ipv4Piece > 255 {
						return address, false
					}
					p++
				}
				address[pieceIndex] = address[pieceIndex]*0x100 + uint16(ipv4Piece)
				numbersSeen++
				if numbersSeen == 2 || numbersSeen == 4 {
					pieceIndex++
				}
			}
			if numbersSeen != 4 {
				return address, false
			}
			break
		} else if at(p) == ':' {
			p++
			if at(p) == eof {
				return address, false
			}
		} else if at(p) != eof {
			return address, false
		}
		address[pieceIndex] = uint16(value)
		pieceIndex++
	}
	if compress != -1 {
		swaps := pieceIndex - compress
		pieceIndex = 7
		for pieceIndex != 0 && swaps > 0 {
			address[pieceIndex], address[compress+swaps-1] = address[compress+swaps-1], address[pieceIndex]
			pieceIndex--
			swaps--
		}
	} else if pieceIndex != 8 {
		return address, false
	}
	return address, true
}

func serializeIPv6(address [8]uint16) string {
	// The first longest run of two or more zero pieces is compressed.
	compress, bestLen := -1, 1
	for i := 0; i < 8; {
		if address[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && address[j] == 0 {
			j++
		}
		if j-i > bestLen {
			compress, bestLen = i, j-i
		}
		i = j
	}
	var b strings.Builder
	ignore0 := false
	for i := 0; i < 8; i++ {
		if ignore0 && address[i] == 0 {
			continue
		}
		ignore0 = false
		if compress == i {
			if i == 0 {
				b.WriteString("::")
			} else {
				b.WriteByte(':')
			}
			ignore0 = true
			continue
		}
		b.WriteString(strconv.FormatUint(uint64(address[i]), 16))
		if i != 7 {
			b.WriteByte(':')
		}
	}
	return b.String()
}
