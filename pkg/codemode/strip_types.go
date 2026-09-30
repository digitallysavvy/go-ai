package codemode

import "strings"

// stripTypeScriptAnnotations removes a conservative, unambiguous subset of
// type-only TypeScript syntax so QuickJS (a JavaScript engine) can execute
// lightly type-annotated code-mode source. This approximates the "or
// type-stripped TypeScript" support the TypeScript SDK gets for free from
// the `run` package's engine (state/parity/sep_23_2026/mcp-workflow-misc.md
// CODE-MODE); it is a small heuristic scanner, not a TypeScript parser or
// a full erasable-syntax implementation.
//
// Handled (matches the ported TypeScript test cases in core.test.ts):
//   - `interface Name { ... }` declarations (balanced braces), removed
//     whole.
//   - Top-level `type Name = ...;` alias declarations.
//   - `satisfies TypeExpr` assertions.
//   - `: TypeExpr` annotations on `const`/`let`/`var` declarations
//     (`const value: number = 7` -> `const value = 7`).
//
// Deliberately NOT handled, to avoid misparsing plain JavaScript object
// literals and call expressions (deferred; see package doc):
//   - `as TypeExpr` assertions (an `as` token is not reserved in
//     JavaScript, so stripping it generically risks mangling other code;
//     only `satisfies`, which has no legitimate JavaScript meaning, is
//     handled).
//   - Function parameter and return type annotations
//     (`function f(x: number): number`) and generic type parameters
//     (`function f<T>(x: T)`), enums, decorators, `declare`/`namespace`
//     blocks, and `import type`.
//
// String, template-literal and comment contents are left untouched.
func stripTypeScriptAnnotations(src string) string {
	tokens := tokenizeTS(src)
	kept := stripTSTokens(tokens)
	var b strings.Builder
	for _, t := range kept {
		b.WriteString(t.text)
	}
	return b.String()
}

type tsToken struct {
	kind string // "ident", "punct", "string", "space", "comment"
	text string
}

func tokenizeTS(src string) []tsToken {
	runes := []rune(src)
	n := len(runes)
	var tokens []tsToken
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

		case c == '"' || c == '\'':
			j := i + 1
			for j < n && runes[j] != c {
				if runes[j] == '\\' && j+1 < n {
					j += 2
				} else {
					j++
				}
			}
			if j < n {
				j++
			}
			tokens = append(tokens, tsToken{"string", string(runes[i:j])})
			i = j

		case c == '`':
			j := i + 1
			depth := 0
			for j < n {
				if runes[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if runes[j] == '`' && depth == 0 {
					j++
					break
				}
				if runes[j] == '$' && j+1 < n && runes[j+1] == '{' {
					depth++
					j += 2
					continue
				}
				if runes[j] == '}' && depth > 0 {
					depth--
					j++
					continue
				}
				j++
			}
			tokens = append(tokens, tsToken{"string", string(runes[i:j])})
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
	}
	return tokens
}

var tsMultiCharOperators = []string{
	">>>=", "===", "!==", "**=", "<<=", ">>=", "&&=", "||=", "??=",
	"=>", "...", "?.", "??", "**", "<<", ">>", ">>>", "&&", "||",
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

// stripTSTokens removes interface/type-alias/satisfies/typed-declaration
// tokens from the stream, returning the tokens to keep.
func stripTSTokens(tokens []tsToken) []tsToken {
	var out []tsToken
	depth := 0 // combined (){}[] nesting depth
	i := 0
	for i < len(tokens) {
		t := tokens[i]

		switch t.kind {
		case "punct":
			switch t.text {
			case "(", "{", "[":
				depth++
			case ")", "}", "]":
				depth--
			}
		case "ident":
			switch t.text {
			case "interface":
				if depth == 0 && !precededByDot(out) {
					i = skipInterfaceDecl(tokens, i)
					continue
				}
			case "type":
				if depth == 0 && !precededByDot(out) && looksLikeTypeAlias(tokens, i) {
					i = skipTypeAlias(tokens, i)
					continue
				}
			case "satisfies":
				if !precededByDot(out) && precededBySignificant(out) {
					i = consumeTypeExpr(tokens, i+1, true)
					continue
				}
			case "const", "let", "var":
				out = append(out, t)
				i++
				i = passThroughTypedDeclaration(tokens, i, &out)
				continue
			}
		}

		out = append(out, t)
		i++
	}
	return out
}

// precededByDot reports whether the last significant (non-space,
// non-comment) emitted token is `.`, which means the following identifier
// is a property name, not a keyword.
func precededByDot(out []tsToken) bool {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].kind == "space" || out[i].kind == "comment" {
			continue
		}
		return out[i].kind == "punct" && out[i].text == "."
	}
	return false
}

// precededBySignificant reports whether at least one non-trivial token has
// been emitted (a `satisfies` at the very start of the source cannot be a
// type assertion).
func precededBySignificant(out []tsToken) bool {
	for _, t := range out {
		if t.kind != "space" && t.kind != "comment" {
			return true
		}
	}
	return false
}

// skipInterfaceDecl returns the index just past a balanced
// `interface ... { ... }` block starting at tokens[i] (the "interface"
// token). If no opening brace is found, only the "interface" token itself
// is skipped.
func skipInterfaceDecl(tokens []tsToken, i int) int {
	j := i + 1
	for j < len(tokens) && !(tokens[j].kind == "punct" && tokens[j].text == "{") {
		j++
	}
	if j >= len(tokens) {
		return i + 1
	}
	depth := 0
	for j < len(tokens) {
		if tokens[j].kind == "punct" {
			switch tokens[j].text {
			case "{":
				depth++
			case "}":
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		j++
	}
	return j
}

// looksLikeTypeAlias reports whether tokens[i] ("type") begins a
// `type Ident =` alias declaration, distinguishing it from an identifier
// named "type" used as a value (e.g. `const type = 1;`, `obj.type`,
// `{ type: 'x' }`).
func looksLikeTypeAlias(tokens []tsToken, i int) bool {
	next := nextSignificant(tokens, i+1)
	if next < 0 || tokens[next].kind != "ident" {
		return false
	}
	eq := nextSignificant(tokens, next+1)
	// Allow simple generic parameters: `type Foo<T> = ...`.
	if eq >= 0 && tokens[eq].kind == "punct" && tokens[eq].text == "<" {
		depth := 0
		for eq < len(tokens) {
			if tokens[eq].kind == "punct" {
				if tokens[eq].text == "<" {
					depth++
				} else if tokens[eq].text == ">" {
					depth--
					if depth == 0 {
						eq = nextSignificant(tokens, eq+1)
						break
					}
				}
			}
			eq++
		}
	}
	return eq >= 0 && tokens[eq].kind == "punct" && tokens[eq].text == "="
}

// skipTypeAlias returns the index just past a `type Name = ...;` statement
// starting at tokens[i] (the "type" token), consuming through the matching
// top-level semicolon (or end of input).
func skipTypeAlias(tokens []tsToken, i int) int {
	j := i + 1
	depth := 0
	for j < len(tokens) {
		t := tokens[j]
		if t.kind == "punct" {
			switch t.text {
			case "(", "{", "[", "<":
				depth++
			case ")", "}", "]", ">":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					return j + 1
				}
			}
		}
		j++
	}
	return j
}

// passThroughTypedDeclaration copies tokens for a `const|let|var` binding
// into *out unchanged, except it removes a single `: TypeExpr` annotation
// immediately following the bound identifier (before `=`, `,` or `;`).
// Returns the index just past what it consumed.
func passThroughTypedDeclaration(tokens []tsToken, i int, out *[]tsToken) int {
	j := nextSignificant(tokens, i)
	if j < 0 || tokens[j].kind != "ident" {
		return i
	}
	// Copy any tokens (including whitespace) up to and including the
	// identifier.
	for k := i; k <= j; k++ {
		*out = append(*out, tokens[k])
	}
	i = j + 1

	colon := nextSignificant(tokens, i)
	if colon < 0 || !(tokens[colon].kind == "punct" && tokens[colon].text == ":") {
		return i
	}
	// Copy whitespace/comments between the identifier and the colon, then
	// drop the colon and the following type expression.
	for k := i; k < colon; k++ {
		*out = append(*out, tokens[k])
	}
	return consumeTypeExpr(tokens, colon+1, false)
}

// consumeTypeExpr skips a type expression starting at tokens[i], balancing
// (), [], {} and <> and stopping at the first top-level `;`, `,`, `)`,
// `}`, `=` or `=>`. When leadingKeywordAlreadyConsumed is false the caller
// is responsible for having already dropped the introducing token (e.g.
// `:`); it exists only for readability at call sites.
func consumeTypeExpr(tokens []tsToken, i int, _ bool) int {
	depth := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == "punct" {
			switch t.text {
			case "(", "{", "[":
				depth++
			case ")", "}", "]":
				if depth == 0 {
					return i
				}
				depth--
			case "<":
				depth++
			case ">":
				if depth > 0 {
					depth--
				}
			case ";", ",", "=", "=>":
				if depth == 0 {
					return i
				}
			}
		}
		i++
	}
	return i
}

func nextSignificant(tokens []tsToken, i int) int {
	for i < len(tokens) {
		if tokens[i].kind != "space" && tokens[i].kind != "comment" {
			return i
		}
		i++
	}
	return -1
}
