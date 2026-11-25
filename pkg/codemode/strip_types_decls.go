package codemode

import (
	"fmt"
	"strings"
)

// skipInterfaceDecl returns the index just past a balanced
// `interface ... { ... }` block starting at tokens[i] (the "interface"
// token), dropping it entirely. If no opening brace is found, only the
// "interface" token itself is skipped.
func skipInterfaceDecl(tokens []tsToken, i int) int {
	j := scanUntilTopLevelBrace(tokens, i+1)
	if j >= len(tokens) {
		return i + 1
	}
	return skipBalancedBraces(tokens, j)
}

// skipBalancedBraces returns the index just past the "}" matching the "{"
// at tokens[openBraceIdx].
func skipBalancedBraces(tokens []tsToken, openBraceIdx int) int {
	depth := 0
	j := openBraceIdx
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
	// Allow type parameters: `type Foo<T> = ...`.
	if eq >= 0 && tokens[eq].kind == "punct" && tokens[eq].text == "<" {
		if end, ok := consumeAngleTypeArgsBalanced(tokens, eq); ok {
			eq = nextSignificant(tokens, end)
		}
	}
	return eq >= 0 && tokens[eq].kind == "punct" && tokens[eq].text == "="
}

// skipTypeAlias returns the index just past a `type Name = ...;` statement
// starting at tokens[i] (the "type" token), consuming through the matching
// top-level semicolon (or end of input), dropping it entirely.
func skipTypeAlias(tokens []tsToken, i int) int {
	eq := nextSignificant(tokens, i+1)
	if eq >= 0 {
		eq = nextSignificant(tokens, eq+1)
	}
	if eq >= 0 && tokens[eq].kind == "punct" && tokens[eq].text == "<" {
		if end, ok := consumeAngleTypeArgsBalanced(tokens, eq); ok {
			eq = nextSignificant(tokens, end)
		}
	}
	if eq < 0 || tokens[eq].kind != "punct" || tokens[eq].text != "=" {
		return scanToStatementEnd(tokens, i+1)
	}
	// The aliased type ends where the type grammar ends (a line break can end
	// it), not at the next `;`.
	end := consumeTypeExpr(tokens, eq+1, false)
	// Node takes a `;` on a later line as the alias's terminator too
	// (`type T = {...}` newline `;(async () => 1)()` erases both).
	if semi := nextSignificant(tokens, end); semi >= 0 && tokens[semi].kind == "punct" && tokens[semi].text == ";" {
		return semi + 1
	}
	return end
}

// consumeOptionalSemicolon returns the index past a `;` that ends the
// statement on the same line as tokens[end-1]; a `;` on a later line is a
// separate empty statement and stays.
func consumeOptionalSemicolon(tokens []tsToken, end int) int {
	for k := end; k < len(tokens); k++ {
		switch tokens[k].kind {
		case "space", "comment":
			if strings.ContainsAny(tokens[k].text, "\n\r") {
				return end
			}
		case "punct":
			if tokens[k].text == ";" {
				return k + 1
			}
			return end
		default:
			return end
		}
	}
	return end
}

// declareVarEnd returns the end of an ambient `declare var/let/const a: T, b:
// U` statement, which a line break ends when no `;` follows.
func declareVarEnd(tokens []tsToken, i int) int {
	for {
		name := nextSignificant(tokens, i)
		if name < 0 || tokens[name].kind != "ident" {
			return scanToStatementEnd(tokens, i)
		}
		i = name + 1
		if c := nextSignificant(tokens, i); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == ":" {
			i = consumeTypeExpr(tokens, c+1, false)
		}
		// `declare const a = 1`: the initializer goes with the declaration.
		if c := nextSignificant(tokens, i); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == "=" {
			i = declareInitializerEnd(tokens, c+1)
		}
		if c := nextSignificant(tokens, i); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == "," {
			i = c + 1
			continue
		}
		return consumeOptionalSemicolon(tokens, i)
	}
}

// declareInitializerEnd returns the index where the initializer expression
// that starts at tokens[i] ends: at the first top-level `,` or `;`, at a
// closing bracket of the enclosing scope, or at a line break that ends the
// statement (ASI). Trailing whitespace and comments are not part of it.
func declareInitializerEnd(tokens []tsToken, i int) int {
	depth := 0
	end := i
	for j := i; j < len(tokens); j++ {
		t := tokens[j]
		switch t.kind {
		case "space", "comment":
			if t.kind == "space" && depth == 0 && strings.ContainsAny(t.text, "\n\r\u2028\u2029") && end > i && asiBreaksBefore(tokens, j) {
				return end
			}
			continue
		case "punct":
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth == 0 {
					return end
				}
				depth--
			case ",", ";":
				if depth == 0 {
					return end
				}
			}
		}
		end = j + 1
	}
	return end
}

// declareFunctionEnd returns the end of an ambient `declare function f<T>(a:
// A): R` statement.
func declareFunctionEnd(tokens []tsToken, i int) int {
	if s := nextSignificant(tokens, i); s >= 0 && tokens[s].kind == "punct" && tokens[s].text == "*" {
		i = s + 1
	}
	if n := nextSignificant(tokens, i); n >= 0 && tokens[n].kind == "ident" {
		i = n + 1
	}
	if lt := nextSignificant(tokens, i); lt >= 0 && tokens[lt].kind == "punct" && tokens[lt].text == "<" {
		if end, ok := consumeAngleTypeArgsBalanced(tokens, lt); ok {
			i = end
		}
	}
	open := nextSignificant(tokens, i)
	if open < 0 || tokens[open].kind != "punct" || tokens[open].text != "(" {
		return scanToStatementEnd(tokens, i)
	}
	closeIdx := matchParen(tokens, open)
	if closeIdx < 0 {
		return scanToStatementEnd(tokens, i)
	}
	i = closeIdx + 1
	if c := nextSignificant(tokens, i); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == ":" {
		i = consumeTypeExpr(tokens, c+1, false)
	}
	return consumeOptionalSemicolon(tokens, i)
}

// scanToStatementEnd returns the index just past the next top-level (at
// bracket depth 0 relative to i) ";" token, or len(tokens) if none is
// found before the end of input.
func scanToStatementEnd(tokens []tsToken, i int) int {
	depth := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == "punct" {
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					return i + 1
				}
			}
		}
		i++
	}
	return i
}

// findBraceOrSemi returns the index of the first "{" or ";" token at or
// after i that is not inside (...), [...] or <...> -- used to locate the body
// or terminator of an ambient `declare ...` statement, whose heritage clause
// can hold an object type: `declare class C extends B<{ a: 1 }> { ... }`.
func findBraceOrSemi(tokens []tsToken, i int) int {
	depth := 0
	for i < len(tokens) {
		if tokens[i].kind == "punct" {
			switch tokens[i].text {
			case "(", "[", "<":
				depth++
			case ")", "]", ">":
				if depth > 0 {
					depth--
				}
			case ">>":
				depth = max(depth-2, 0)
			case ">>>":
				depth = max(depth-3, 0)
			case "{":
				if depth == 0 {
					return i
				}
				depth++
			case "}":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					return i
				}
			}
		}
		i++
	}
	return len(tokens)
}

// handleDeclare drops an ambient `declare ...` statement entirely: it has
// no runtime representation. tokens[i] is the "declare" token.
func handleDeclare(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	next := nextSignificant(tokens, i+1)
	if next < 0 || tokens[next].kind != "ident" {
		*out = append(*out, tokens[i])
		return i + 1, nil
	}
	switch tokens[next].text {
	case "const":
		if nextIdentIs(tokens, next+1, "enum") {
			// `declare const enum E { ... }`: ambient, erased like `declare enum`.
			idx := findBraceOrSemi(tokens, next+1)
			if idx < len(tokens) && tokens[idx].kind == "punct" && tokens[idx].text == "{" {
				return skipBalancedBraces(tokens, idx), nil
			}
			return scanToStatementEnd(tokens, idx), nil
		}
		return declareVarEnd(tokens, next+1), nil
	case "var", "let":
		return declareVarEnd(tokens, next+1), nil
	case "type":
		// `declare type X = ...;`: an alias, erased whole.
		if looksLikeTypeAlias(tokens, next) {
			return skipTypeAlias(tokens, next), nil
		}
		*out = append(*out, tokens[i])
		return i + 1, nil
	case "function":
		return declareFunctionEnd(tokens, next+1), nil
	case "class", "interface", "namespace", "module", "enum", "global":
		idx := findBraceOrSemi(tokens, next+1)
		if idx < len(tokens) && tokens[idx].kind == "punct" && tokens[idx].text == "{" {
			return skipBalancedBraces(tokens, idx), nil
		}
		return scanToStatementEnd(tokens, idx), nil
	case "abstract":
		// `declare abstract class C { ... }`: erased entirely, exactly like
		// a plain `declare class` (verified directly against Node -- see
		// strip_types_test.go). "abstract" is only a class-level modifier
		// here (the `declare ...` grammar has no other use for it), so
		// anything other than "abstract class" falls through to default.
		if nextIdentIs(tokens, next+1, "class") {
			classTok := nextSignificant(tokens, next+1)
			idx := findBraceOrSemi(tokens, classTok+1)
			if idx < len(tokens) && tokens[idx].kind == "punct" && tokens[idx].text == "{" {
				return skipBalancedBraces(tokens, idx), nil
			}
			return scanToStatementEnd(tokens, idx), nil
		}
		*out = append(*out, tokens[i])
		return i + 1, nil
	default:
		*out = append(*out, tokens[i])
		return i + 1, nil
	}
}

// handleImportExport rejects a static `import`/`export` statement. code-mode
// wraps the snippet in a function body before stripping (see strip_types.go),
// where neither can appear, so Node's stripTypeScriptTypes throws
// ERR_INVALID_TYPESCRIPT_SYNTAX for them -- `import type` and `export type`
// included -- and the whole snippet is handed to the engine unstripped, which
// reports the SyntaxError. A dynamic `import(...)` or `import.meta` is an
// expression and is copied through.
func handleImportExport(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	if tokens[i].text == "import" {
		if next := nextSignificant(tokens, i+1); next >= 0 && tokens[next].kind == "punct" && (tokens[next].text == "(" || tokens[next].text == ".") {
			*out = append(*out, tokens[i])
			return i + 1, nil
		}
	}
	return i, fmt.Errorf("code mode source uses %s outside a module", tokens[i].text)
}
