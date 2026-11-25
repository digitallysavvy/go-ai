package urlnorm

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/secure/bidirule"
	"golang.org/x/text/unicode/bidi"
	"golang.org/x/text/unicode/norm"
)

// asciiProfile is UTS #46 ToASCII with the WHATWG "domain to ASCII" flags
// (CheckHyphens=false, CheckJoiners=true, UseSTD3ASCIIRules=false,
// Transitional_Processing=false, VerifyDnsLength=false). CheckBidi is done
// separately by checkBidi.
var asciiProfile = idna.New(
	idna.MapForLookup(),
	idna.Transitional(false),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.CheckJoiners(true),
	idna.VerifyDNSLength(false),
)

// mapProfile performs only the UTS #46 mapping of a single code point.
var mapProfile = idna.New(
	idna.MapForLookup(),
	idna.Transitional(false),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.CheckJoiners(false),
)

// domainToASCII is the WHATWG "domain to ASCII" algorithm (beStrict=false),
// matching Node's URL implementation (Ada):
//   - An all-ASCII domain takes Ada's fast path: it is only lowercased, so
//     "xn--" labels are not Punycode-validated ("http://xn--a.com" parses).
//   - Otherwise the domain goes through UTS #46. Checks that
//     golang.org/x/net/idna does not make are added (an "xn--" label, after
//     mapping, that has non-ASCII code points or decodes to nothing or to
//     ASCII only is an error), and the Bidi rule (RFC 5893) is applied, as
//     Ada does, only to labels that contain an R, AL or AN code point.
//
// Known differences, both only for a domain with a non-ASCII code point and
// an "xn--" label after mapping (found by fuzzing against Node v24.20.0):
//   - "xn---xii" (the only hyphen after the prefix comes first) is valid RFC
//     3492 Punycode that Node accepts and x/net/idna rejects.
//   - "xn--xn--xiiffxn--255ab" decodes under RFC 3492 (x/net, node:punycode)
//     but Ada's decoder rejects it, so Node fails where Parse succeeds.
func domainToASCII(domain string) (string, error) {
	var ascii string
	if isASCII(domain) {
		ascii = strings.ToLower(domain)
	} else {
		var err error
		ascii, err = asciiProfile.ToASCII(domain)
		if err != nil {
			return "", ErrInvalidURL
		}
		if !checkMappedLabels(uts46Map(domain)) {
			return "", ErrInvalidURL
		}
	}
	if ascii == "" {
		return "", ErrInvalidURL
	}
	for _, c := range ascii {
		if isForbiddenDomainCodePoint(c) {
			return "", ErrInvalidURL
		}
	}
	return ascii, nil
}

// uts46Map is UTS #46 Processing steps 1-2: map each code point, then
// normalize to NFC. x/net/idna does not expose its mapping step, so each code
// point is mapped on its own (no single code point maps to a string that
// starts a Punycode label, so none is decoded).
func uts46Map(domain string) string {
	var b strings.Builder
	for _, r := range domain {
		if r < utf8.RuneSelf {
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteRune(r)
			continue
		}
		mapped, _ := mapProfile.ToUnicode(string(r))
		b.WriteString(mapped)
	}
	return norm.NFC.String(b.String())
}

// checkMappedLabels runs the UTS #46 step 4 Punycode checks and the
// per-label Bidi rule on the mapped domain.
func checkMappedLabels(mapped string) bool {
	for _, label := range strings.Split(mapped, ".") {
		if strings.HasPrefix(label, "xn--") {
			if !isASCII(label) {
				return false
			}
			decoded, err := idna.Punycode.ToUnicode(label)
			if err != nil || decoded == "" || isASCII(decoded) {
				return false
			}
			label = decoded
		}
		if hasRTL(label) && !bidirule.ValidString(label) {
			return false
		}
	}
	return true
}

func hasRTL(label string) bool {
	for _, r := range label {
		if r < utf8.RuneSelf {
			continue
		}
		props, _ := bidi.LookupRune(r)
		switch props.Class() {
		case bidi.R, bidi.AL, bidi.AN:
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
