package codemode

import "strings"

// stripTypeScriptAnnotations removes type-only TypeScript syntax from
// code-mode source so it can run as plain JavaScript in QuickJS.
//
// This mirrors what TypeScript code-mode actually does before execution.
// The `code-mode` package delegates execution to the `run` package
// (https://www.npmjs.com/package/run), whose
// dist/utils/source-cache.js#stripSnippetTypes wraps the raw snippet as
// `async function __runUser__(){ <snippet> }` and calls Node's built-in
// `node:module` `stripTypeScriptTypes(source)` (Node's "type stripping"
// feature, https://nodejs.org/api/typescript.html, backed by the Amaro/swc
// TypeScript parser) in its default "strip" mode, then slices the wrapper
// back off. That function erases type-only syntax in place -- replacing it
// with the equivalent amount of whitespace so line/column numbers in stack
// traces are preserved -- and throws:
//   - ERR_INVALID_TYPESCRIPT_SYNTAX for a genuine parse error (including
//     `import`/`export`, which cannot appear inside the function-body
//     wrapper regardless of TypeScript syntax), or
//   - ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX for TypeScript syntax that is
//     erasable in principle but not supported by Node's "strip-only" mode
//     without full transformation: enums, namespaces/modules with a body,
//     and constructor parameter properties.
//
// Critically, `stripSnippetTypes` catches *any* error from
// `stripTypeScriptTypes` and falls back to returning the snippet
// unmodified: `catch { return source; }`. So in practice a snippet
// containing unsupported syntax is never rejected by the stripper itself --
// it is handed to the JavaScript engine unstripped, where the leftover
// TypeScript-only syntax (an invalid token as far as the engine is
// concerned) fails with a normal JavaScript SyntaxError instead of a
// TypeScript-specific message.
//
// This Go port matches that fallback exactly: on any error (enums,
// namespaces/modules with a body, constructor parameter properties, or
// anything else this scanner cannot confidently strip -- an unbalanced
// construct, or TypeScript syntax outside the set below), it returns the
// original source unmodified alongside the error, and its one caller
// (RunCodeMode) discards the error and runs that unmodified source, letting
// QuickJS report an ordinary syntax error if it truly isn't valid
// JavaScript -- never a stripper-specific rejection.
//
// Supported erasable syntax (verified against Node's stripTypeScriptTypes
// directly -- see the package's strip_types_test.go for the exact cases):
//   - Type annotations on `const`/`let`/`var` declarators (including
//     multiple comma-separated declarators), function/method parameters
//     (including default values, destructuring, and rest parameters), and
//     function/method/arrow return types.
//   - `as` and `satisfies` type assertions, including chained
//     (`x as A as B`) and `as const`.
//   - Type parameters/arguments on function and class declarations, arrow
//     functions, and call/`new` expressions (`f<T>()`, `new Box<T>()`),
//     disambiguated from `<`/`>` comparisons.
//   - `interface Name { ... }` and `type Name = ...;` declarations
//     (top-level only, not nested inside an expression).
//   - Non-null assertions (`foo!.bar`) and definite assignment assertions
//     (`let x!: number;`, a class field `x!: number;`).
//   - Optional markers on parameters and class fields (`x?: number`).
//   - `import type ...;` / `export type ...;` statements.
//   - Class member access modifiers (`public`/`private`/`protected`) and
//     `readonly`/`override`/`declare`, plus bodyless (abstract or overload)
//     member/function signatures, which are erased entirely.
//   - `declare` ambient statements (`declare var/let/const/function/class`),
//     erased entirely.
//
// Known gap: a generic arrow function written as a bare expression, e.g.
// `const f = <T,>(x: T) => x;`, is not recognized (declaration-position
// generics on `function`/`class`, and generics on a call/`new` expression,
// are). Type parameters immediately followed by `(` at the very start of
// an expression are inherently ambiguous with a JSX element in TypeScript
// itself (resolved there only by the `.tsx` vs `.ts` file extension, which
// code-mode snippets don't have); detecting it heuristically would risk
// misreading a real less-than comparison. Code-mode snippets needing
// generics on an arrow function can use a named `function` declaration
// instead, which this stripper fully supports.
//
// String, template-literal, regular-expression-literal, and comment
// contents are never inspected for TypeScript syntax.
func stripTypeScriptAnnotations(src string) (string, error) {
	tokens := tokenizeTS(src)
	kept, err := stripTSTokens(tokens)
	if err != nil {
		return src, err
	}
	var b strings.Builder
	for _, t := range kept {
		b.WriteString(t.text)
	}
	return b.String(), nil
}

// tsToken is one lexical token of (possibly-TypeScript) source.
type tsToken struct {
	kind string // "ident", "number", "string", "regex", "punct", "space", "comment"
	text string
}

// tokenizeTS splits src into tokens, correctly skipping over the contents
// of strings, template literals (including nested `${...}` interpolations,
// which may themselves contain strings/templates), regular expression
// literals, and line/block comments so TypeScript-syntax detection never
// looks inside them.
func tokenizeTS(src string) []tsToken {
	runes := []rune(src)
	n := len(runes)
	var tokens []tsToken
	// prevSignificant is the kind/text of the last non-space/non-comment
	// token, used to disambiguate a leading `/` as a regex literal (as
	// opposed to division) the same way real JS lexers do.
	var prevSignificant *tsToken
	i := 0
	for i < n {
		c := runes[i]
		switch {
		case c == '/' && i+1 < n && runes[i+1] == '/':
			j := i + 2
			for j < n && runes[j] != '\n' {
				j++
			}
			tokens = append(tokens, tsToken{"comment", string(runes[i:j])})
			i = j

		case c == '/' && i+1 < n && runes[i+1] == '*':
			j := i + 2
			for j+1 < n && !(runes[j] == '*' && runes[j+1] == '/') {
				j++
			}
			end := j + 2
			if end > n {
				end = n
			}
			tokens = append(tokens, tsToken{"comment", string(runes[i:end])})
			i = end

		case c == '/' && regexAllowedAfter(prevSignificant):
			if end, ok := scanRegexLiteral(runes, i); ok {
				tokens = append(tokens, tsToken{"regex", string(runes[i:end])})
				i = end
				prevSignificant = &tokens[len(tokens)-1]
				continue
			}
			fallthrough

		case c == '"' || c == '\'':
			j := scanStringLiteral(runes, i)
			tokens = append(tokens, tsToken{"string", string(runes[i:j])})
			i = j

		case c == '`':
			j := scanTemplateLiteral(runes, i)
			tokens = append(tokens, tsToken{"string", string(runes[i:j])})
			i = j

		case isDigit(c) || (c == '.' && i+1 < n && isDigit(runes[i+1])):
			j := scanNumber(runes, i)
			tokens = append(tokens, tsToken{"number", string(runes[i:j])})
			i = j

		case isTSIdentStart(c):
			j := i + 1
			for j < n && isTSIdentPart(runes[j]) {
				j++
			}
			tokens = append(tokens, tsToken{"ident", string(runes[i:j])})
			i = j

		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			j := i
			for j < n && (runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\r' || runes[j] == '\n') {
				j++
			}
			tokens = append(tokens, tsToken{"space", string(runes[i:j])})
			i = j

		default:
			matched := matchTSOperator(runes[i:])
			if matched != "" {
				tokens = append(tokens, tsToken{"punct", matched})
				i += len([]rune(matched))
			} else {
				tokens = append(tokens, tsToken{"punct", string(c)})
				i++
			}
		}

		if len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			if last.kind != "space" && last.kind != "comment" {
				prevSignificant = &tokens[len(tokens)-1]
			}
		}
	}
	return tokens
}

// scanStringLiteral scans a single- or double-quoted string starting at
// runes[i] and returns the index just past its closing quote (or past the
// end of input, for an unterminated string).
func scanStringLiteral(runes []rune, i int) int {
	n := len(runes)
	quote := runes[i]
	j := i + 1
	for j < n && runes[j] != quote {
		if runes[j] == '\\' && j+1 < n {
			j += 2
			continue
		}
		j++
	}
	if j < n {
		j++
	}
	return j
}

// scanTemplateLiteral scans a template literal starting at runes[i] == '`'
// and returns the index just past its closing backtick. Interpolations
// (`${...}`) are scanned with scanBalancedExpr, which is itself aware of
// nested strings, template literals, and comments, so a `}`, backtick, or
// quote inside a nested string/template cannot prematurely close the
// interpolation or the outer template.
func scanTemplateLiteral(runes []rune, i int) int {
	n := len(runes)
	j := i + 1
	for j < n {
		c := runes[j]
		switch {
		case c == '\\' && j+1 < n:
			j += 2
		case c == '`':
			return j + 1
		case c == '$' && j+1 < n && runes[j+1] == '{':
			j = scanBalancedExpr(runes, j+2)
		default:
			j++
		}
	}
	return j
}

// scanBalancedExpr scans an interpolation expression's tokens starting just
// after its opening `${`, returning the index just past the matching `}`.
// It tracks nested `(`, `[`, `{` bracket depth and recognizes nested
// strings, template literals, and comments so their contents can't disturb
// that count.
func scanBalancedExpr(runes []rune, j int) int {
	n := len(runes)
	depth := 0
	for j < n {
		c := runes[j]
		switch {
		case c == '\'' || c == '"':
			j = scanStringLiteral(runes, j)
		case c == '`':
			j = scanTemplateLiteral(runes, j)
		case c == '/' && j+1 < n && runes[j+1] == '/':
			for j < n && runes[j] != '\n' {
				j++
			}
		case c == '/' && j+1 < n && runes[j+1] == '*':
			j += 2
			for j+1 < n && !(runes[j] == '*' && runes[j+1] == '/') {
				j++
			}
			j += 2
			if j > n {
				j = n
			}
		case c == '{' || c == '(' || c == '[':
			depth++
			j++
		case c == '}':
			if depth == 0 {
				return j + 1
			}
			depth--
			j++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
			j++
		default:
			j++
		}
	}
	return j
}

// scanRegexLiteral attempts to scan a regular expression literal starting
// at runes[i] == '/'. It respects character classes (`[...]`), where an
// unescaped `/` does not terminate the literal, and returns
// (indexPastFlags, true) on success or (_, false) if this `/` cannot be
// parsed as a regex (e.g. it is unterminated before a line break).
func scanRegexLiteral(runes []rune, i int) (int, bool) {
	n := len(runes)
	j := i + 1
	inClass := false
	for j < n {
		c := runes[j]
		if c == '\n' {
			return 0, false
		}
		if c == '\\' && j+1 < n {
			j += 2
			continue
		}
		if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '/' && !inClass {
			j++
			for j < n && isTSIdentPart(runes[j]) {
				j++
			}
			return j, true
		}
		j++
	}
	return 0, false
}

// regexAllowedAfter reports whether a `/` immediately following prev could
// start a regular expression literal, as opposed to being a division or
// division-assignment operator. It uses the same approximation real JS
// lexers use: a `/` starts a regex unless the previous significant token
// was something a value expression can end with (an identifier that isn't
// a keyword expecting a following expression, a number, a string/template,
// a regex, or a closing `)`/`]`).
func regexAllowedAfter(prev *tsToken) bool {
	if prev == nil {
		return true
	}
	switch prev.kind {
	case "number", "string", "regex":
		return false
	case "ident":
		return isRegexPrecedingKeyword(prev.text)
	case "punct":
		switch prev.text {
		case ")", "]":
			return false
		default:
			return true
		}
	}
	return true
}

var regexPrecedingKeywords = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true,
	"of": true, "new": true, "delete": true, "void": true, "throw": true,
	"case": true, "do": true, "else": true, "yield": true, "await": true,
}

func isRegexPrecedingKeyword(ident string) bool {
	return regexPrecedingKeywords[ident]
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// scanNumber scans a numeric literal (decimal, hex/octal/binary with a
// `0x`/`0o`/`0b` prefix, decimal point, exponent, numeric separators `_`,
// and a trailing BigInt `n` suffix) starting at runes[i].
func scanNumber(runes []rune, i int) int {
	n := len(runes)
	j := i
	if runes[j] == '0' && j+1 < n && (runes[j+1] == 'x' || runes[j+1] == 'X' || runes[j+1] == 'o' || runes[j+1] == 'O' || runes[j+1] == 'b' || runes[j+1] == 'B') {
		j += 2
		for j < n && (isHexDigit(runes[j]) || runes[j] == '_') {
			j++
		}
	} else {
		for j < n && (isDigit(runes[j]) || runes[j] == '_') {
			j++
		}
		if j < n && runes[j] == '.' {
			j++
			for j < n && (isDigit(runes[j]) || runes[j] == '_') {
				j++
			}
		}
		if j < n && (runes[j] == 'e' || runes[j] == 'E') {
			k := j + 1
			if k < n && (runes[k] == '+' || runes[k] == '-') {
				k++
			}
			if k < n && isDigit(runes[k]) {
				j = k
				for j < n && isDigit(runes[j]) {
					j++
				}
			}
		}
	}
	if j < n && runes[j] == 'n' {
		j++
	}
	return j
}

func isHexDigit(c rune) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

var tsMultiCharOperators = []string{
	">>>=", "===", "!==", "**=", "<<=", ">>=", "&&=", "||=", "??=",
	"=>", "...", "?.", "??", "**", "<<", ">>>", ">>", "&&", "||",
	"==", "!=", "<=", ">=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
}

func matchTSOperator(remaining []rune) string {
	for _, op := range tsMultiCharOperators {
		opRunes := []rune(op)
		if len(remaining) < len(opRunes) {
			continue
		}
		if string(remaining[:len(opRunes)]) == op {
			return op
		}
	}
	return ""
}

func isTSIdentStart(c rune) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isTSIdentPart(c rune) bool {
	return isTSIdentStart(c) || (c >= '0' && c <= '9')
}
