package providerutils

// Building blocks for porting TS regular expressions to Go RE2 with the same
// matches. RE2 `\s` is ASCII only ([\t\n\f\r ]) and its `.` matches \r and
// U+2028/U+2029; a JS regexp without the u flag also never case-folds an
// ASCII letter to a non-ASCII one, while Go `(?i)` folds s to U+017F and k
// (and `\w`) to U+212A. Write a TS /s/i letter as `(?-i:[sS])` (and k as
// `(?-i:[kK])`) where the input can hold those runes.

// JSRegexpSpaceChars is the body of JavaScript's regexp `\s` class
// (WhiteSpace and LineTerminator, the runes IsJSWhiteSpace reports), for use
// inside a bracket expression: TS /[^";\s]/ is `[^";` + JSRegexpSpaceChars +
// `]`.
const JSRegexpSpaceChars = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// JSRegexpSpace is JavaScript's regexp `\s`.
const JSRegexpSpace = `[` + JSRegexpSpaceChars + `]`

// JSRegexpDot is JavaScript's regexp `.` without the s flag: any rune but a
// line terminator.
const JSRegexpDot = `[^\n\r\x{2028}\x{2029}]`
