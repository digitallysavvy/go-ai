package codemode

// skipInterfaceDecl returns the index just past a balanced
// `interface ... { ... }` block starting at tokens[i] (the "interface"
// token), dropping it entirely. If no opening brace is found, only the
// "interface" token itself is skipped.
func skipInterfaceDecl(tokens []tsToken, i int) int {
	j := i + 1
	for j < len(tokens) && (tokens[j].kind != "punct" || tokens[j].text != "{") {
		j++
	}
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
	return scanToStatementEnd(tokens, i+1)
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

// handleImportExport erases a whole `import type ...;` / `export type
// ...;` statement (which has no runtime effect). Any other `import`/
// `export` statement is left for the caller to hand to the JavaScript
// engine unchanged -- it isn't TypeScript-only syntax this stripper
// removes, and (like TypeScript code-mode, which wraps snippets as a
// function body before stripping -- see strip_types.go) it will fail to
// parse in the sandbox the same way it would upstream, since import/export
// declarations cannot appear inside a function body.
func handleImportExport(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	next := nextSignificant(tokens, i+1)
	if next >= 0 && tokens[next].kind == "ident" && tokens[next].text == "type" {
		return scanToStatementEnd(tokens, next+1), nil
	}
	*out = append(*out, tokens[i])
	return i + 1, nil
}
