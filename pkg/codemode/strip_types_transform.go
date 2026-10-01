package codemode

import "fmt"

// errUnsupportedTSSyntax aborts a strip attempt for TypeScript syntax that
// Node's stripTypeScriptTypes recognizes but rejects even in principle,
// throwing ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX: enums, namespaces/modules
// with a body, and constructor parameter properties. None of these erase
// to nothing the way a type annotation does, so there is no safe
// type-stripping translation to plain JavaScript.
//
// This is a plain, unexported error -- not a CodeModeError type -- because,
// exactly like every other error stripTSTokens can return, its caller
// (stripTypeScriptAnnotations) never surfaces it: on any error it returns
// the original source unmodified (see strip_types.go), and RunCodeMode
// discards the error entirely and runs that unmodified source, letting
// QuickJS report the failure as an ordinary syntax error -- mirroring
// stripSnippetTypes's `catch { return source; }` exactly, including for
// this case. A named exported error type would imply Go rejects this
// syntax up front, which is precisely the divergence from TypeScript this
// mirrors away.
func errUnsupportedTSSyntax(construct string) error {
	return fmt.Errorf("code mode source uses unsupported TypeScript syntax (%s); it cannot be stripped to plain JavaScript", construct)
}

// stripTSTokens is the entry point used by stripTypeScriptAnnotations: it
// runs stripBody over the whole token stream and returns the kept tokens.
func stripTSTokens(tokens []tsToken) ([]tsToken, error) {
	var out []tsToken
	end, err := stripBody(tokens, 0, &out, nil)
	if err != nil {
		return nil, err
	}
	if end < len(tokens) {
		// A stray top-level closing bracket or similar malformed input
		// stopped the scan early; copy the remainder through unchanged
		// rather than silently dropping it.
		out = append(out, tokens[end:]...)
	}
	return out, nil
}

// stripBody is the recursive workhorse of the TypeScript stripper. It
// copies tokens[i:] to *out, transforming recognized TypeScript syntax
// along the way, until it reaches -- at bracket nesting depth 0 relative to
// i -- a token for which stop returns true (stop may be nil, meaning never
// stop early), or the end of input. It returns the index of the token that
// satisfied stop (not consumed) or len(tokens).
//
// Brackets are handled by direct recursion (stripBody calls itself for the
// contents of every "(", "{", and "[" it is not special-casing), so the
// caller never needs to track nesting depth itself; each recursive call's
// own "local depth 0" is exactly its own bracket's interior.
func stripBody(tokens []tsToken, i int, out *[]tsToken, stop func(tsToken) bool) (int, error) {
	for i < len(tokens) {
		t := tokens[i]

		if stop != nil && t.kind != "space" && t.kind != "comment" && stop(t) {
			return i, nil
		}

		switch t.kind {
		case "space", "comment", "string", "number", "regex":
			*out = append(*out, t)
			i++
			continue
		}

		if t.kind == "punct" {
			switch t.text {
			case "(":
				closeIdx := matchParen(tokens, i)
				if closeIdx >= 0 && isArrowParamList(tokens, closeIdx) {
					var err error
					i, err = stripParamList(tokens, i, out, false)
					if err != nil {
						return i, err
					}
					i = stripOptionalReturnType(tokens, i, out)
					continue
				}
				*out = append(*out, t)
				end, err := stripBody(tokens, i+1, out, stopAtPunct(")"))
				if err != nil {
					return end, err
				}
				if end < len(tokens) {
					*out = append(*out, tokens[end])
					end++
				}
				i = end
				continue
			case "{":
				*out = append(*out, t)
				end, err := stripBody(tokens, i+1, out, stopAtPunct("}"))
				if err != nil {
					return end, err
				}
				if end < len(tokens) {
					*out = append(*out, tokens[end])
					end++
				}
				i = end
				continue
			case "[":
				*out = append(*out, t)
				end, err := stripBody(tokens, i+1, out, stopAtPunct("]"))
				if err != nil {
					return end, err
				}
				if end < len(tokens) {
					*out = append(*out, tokens[end])
					end++
				}
				i = end
				continue
			case "!":
				// A postfix non-null assertion (`foo!.bar`) is dropped
				// when it follows something that ends an expression; a
				// prefix logical NOT (`!x`, `!!x`) never does (its operand
				// hasn't been scanned yet, so the preceding token is
				// whatever came before the `!` itself -- an operator,
				// `(`, `,`, `return`, etc., none of which ends an
				// expression). `!=`/`!==` are already single tokens from
				// the tokenizer, so a bare "!" here can never be part of
				// one.
				if endsExpression(*out) {
					i++
					continue
				}
				*out = append(*out, t)
				i++
				continue
			case "<":
				// A generic arrow function's own type parameter list, e.g.
				// `<T,>(x: T) => x` or `<T>(x: T): T => x` (see
				// tryGenericArrowTypeParams). Only tried where a new
				// expression can start -- the same guard `as`/`satisfies`
				// use in reverse -- since a `<` that follows something
				// ending an expression is an ordinary less-than comparison,
				// never generics.
				if !endsExpression(*out) {
					if open, ok := tryGenericArrowTypeParams(tokens, i); ok {
						var err error
						i, err = stripParamList(tokens, open, out, false)
						if err != nil {
							return i, err
						}
						i = stripOptionalReturnType(tokens, i, out)
						continue
					}
				}
				*out = append(*out, t)
				i++
				continue
			default:
				*out = append(*out, t)
				i++
				continue
			}
		}

		// t.kind == "ident"
		if !precededByDot(*out) {
			switch t.text {
			case "interface":
				if isStatementStart(*out) {
					i = skipInterfaceDecl(tokens, i)
					continue
				}
			case "type":
				if isStatementStart(*out) && looksLikeTypeAlias(tokens, i) {
					i = skipTypeAlias(tokens, i)
					continue
				}
			case "satisfies", "as":
				if endsExpression(*out) {
					i = consumeTypeExpr(tokens, i+1, false)
					continue
				}
			case "enum":
				if isStatementStart(*out) {
					return i, errUnsupportedTSSyntax("enum")
				}
			case "namespace", "module":
				if isStatementStart(*out) && looksLikeNamespaceDecl(tokens, i) {
					return i, errUnsupportedTSSyntax(t.text)
				}
			case "abstract":
				// `abstract class C { ... }`: the class-level modifier
				// (distinct from an `abstract` member inside a class
				// body, handled by stripClassMember).
				if nextIdentIs(tokens, i+1, "class") {
					i = nextSignificant(tokens, i+1)
					continue
				}
			case "declare":
				if isStatementStart(*out) {
					var err error
					i, err = handleDeclare(tokens, i, out)
					if err != nil {
						return i, err
					}
					continue
				}
			case "import", "export":
				if isStatementStart(*out) {
					var err error
					i, err = handleImportExport(tokens, i, out)
					if err != nil {
						return i, err
					}
					continue
				}
			case "const":
				if nextIdentIs(tokens, i+1, "enum") {
					return i, errUnsupportedTSSyntax("const enum")
				}
				*out = append(*out, t)
				i++
				var err error
				i, err = handleDeclaratorList(tokens, i, out)
				if err != nil {
					return i, err
				}
				continue
			case "let", "var":
				*out = append(*out, t)
				i++
				var err error
				i, err = handleDeclaratorList(tokens, i, out)
				if err != nil {
					return i, err
				}
				continue
			case "function":
				var err error
				i, err = handleFunctionSignature(tokens, i, out)
				if err != nil {
					return i, err
				}
				continue
			case "class":
				var err error
				i, err = handleClassDecl(tokens, i, out)
				if err != nil {
					return i, err
				}
				continue
			}
		}

		// Plain identifier (including a keyword whose guard above didn't
		// match, e.g. `const interface = 1;`): copy it through, then check
		// for call-expression generics (`ident<T>(...)`), which can follow
		// any identifier -- including a property access like
		// `obj.method<T>(...)`.
		*out = append(*out, t)
		i++
		if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "<" {
			if end, ok := consumeCallGenerics(tokens, p); ok {
				for k := i; k < p; k++ {
					*out = append(*out, tokens[k])
				}
				i = end
			}
		}
	}
	return i, nil
}

func stopAtPunct(text string) func(tsToken) bool {
	return func(t tsToken) bool { return t.kind == "punct" && t.text == text }
}

func stopAtAny(texts ...string) func(tsToken) bool {
	set := make(map[string]bool, len(texts))
	for _, t := range texts {
		set[t] = true
	}
	return func(t tsToken) bool { return t.kind == "punct" && set[t.text] }
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

// precededByDot reports whether the last significant emitted token is `.`,
// meaning the identifier about to be processed is a property name, not a
// keyword.
func precededByDot(out []tsToken) bool {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].kind == "space" || out[i].kind == "comment" {
			continue
		}
		return out[i].kind == "punct" && out[i].text == "."
	}
	return false
}

// isStatementStart reports whether the position right after the last
// emitted significant token can begin a new statement: the very start of
// the scan, right after `;` or `}`, or right after a `{` that itself opens
// a block rather than an object literal (see looksLikeBlockOpen).
// Declaration-only keywords (interface/type/enum/namespace/declare/
// import/export) are only recognized in this position, matching where
// TypeScript itself requires them -- in particular, `{ interface: 1 }`
// must never be mistaken for an interface declaration just because it
// follows an opening `{`.
func isStatementStart(out []tsToken) bool {
	i := len(out) - 1
	for i >= 0 && (out[i].kind == "space" || out[i].kind == "comment") {
		i--
	}
	if i < 0 {
		return true
	}
	if out[i].kind != "punct" {
		return false
	}
	switch out[i].text {
	case ";", "}":
		return true
	case "{":
		return looksLikeBlockOpen(out[:i])
	}
	return false
}

// looksLikeBlockOpen reports whether a "{" following the (already
// trailing-trimmed of nothing -- callers pass out[:i]) token stream out
// opens a block statement, as opposed to an object literal. It mirrors the
// same disambiguation every JS lexer needs (most visibly for ASI): a block
// follows statement-level punctuation/keywords (`;`, `{`, `}`, `:`, `)`
// closing an if/for/while/switch/function-parameter-list, `=>`, `else`,
// `do`, `try`, `finally`) or starts the program/a function body; anything
// else (`=`, `(`, `[`, `,`, `return`, an operator, ...) means the `{`
// starts a fresh expression, most commonly an object literal.
func looksLikeBlockOpen(out []tsToken) bool {
	i := len(out) - 1
	for i >= 0 && (out[i].kind == "space" || out[i].kind == "comment") {
		i--
	}
	if i < 0 {
		return true
	}
	t := out[i]
	if t.kind == "ident" {
		switch t.text {
		case "else", "do", "try", "finally":
			return true
		}
		return false
	}
	if t.kind != "punct" {
		return false
	}
	switch t.text {
	case ";", "{", "}", ":", ")", "=>":
		return true
	}
	return false
}

// endsExpression reports whether the last significant emitted token could
// be the end of a value expression, which gates `as`/`satisfies`
// recognition: `const as = 1;` (as ends a declaration keyword, not an
// expression) must not be mistaken for a type assertion.
func endsExpression(out []tsToken) bool {
	for i := len(out) - 1; i >= 0; i-- {
		t := out[i]
		if t.kind == "space" || t.kind == "comment" {
			continue
		}
		switch t.kind {
		case "ident":
			return !isExpressionPrefixKeyword(t.text)
		case "number", "string", "regex":
			return true
		case "punct":
			switch t.text {
			case ")", "]", "}", "!":
				return true
			}
			return false
		}
		return false
	}
	return false
}

var expressionPrefixKeywords = map[string]bool{
	"const": true, "let": true, "var": true, "function": true, "class": true,
	"interface": true, "type": true, "return": true, "typeof": true,
	"new": true, "in": true, "yield": true, "case": true, "delete": true,
	"void": true, "throw": true, "do": true, "else": true, "extends": true,
	"implements": true, "import": true, "export": true, "default": true,
	"satisfies": true, "as": true, "enum": true, "namespace": true,
	"declare": true, "abstract": true, "readonly": true, "public": true,
	"private": true, "protected": true, "static": true, "instanceof": true,
}

func isExpressionPrefixKeyword(ident string) bool { return expressionPrefixKeywords[ident] }

func nextIdentIs(tokens []tsToken, i int, text string) bool {
	j := nextSignificant(tokens, i)
	return j >= 0 && tokens[j].kind == "ident" && tokens[j].text == text
}

func looksLikeNamespaceDecl(tokens []tsToken, i int) bool {
	next := nextSignificant(tokens, i+1)
	if next < 0 || (tokens[next].kind != "ident" && tokens[next].kind != "string") {
		return false
	}
	after := nextSignificant(tokens, next+1)
	return after >= 0 && tokens[after].kind == "punct" && tokens[after].text == "{"
}
