package codemode

import "fmt"

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
	if semi := nextSignificant(tokens, end); semi >= 0 && tokens[semi].kind == "punct" && tokens[semi].text == ";" {
		return semi + 1
	}
	return end
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
// after i (a plain forward scan, not bracket-depth aware -- used only to
// locate the start of an ambient `declare ...` body/terminator, where type
// parameter lists containing neither are the only thing that could precede
// it).
func findBraceOrSemi(tokens []tsToken, i int) int {
	for i < len(tokens) {
		if tokens[i].kind == "punct" && (tokens[i].text == "{" || tokens[i].text == ";") {
			return i
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
	case "var", "let", "const", "function":
		return scanToStatementEnd(tokens, next+1), nil
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
