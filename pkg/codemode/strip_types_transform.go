package codemode

import (
	"fmt"
	"strings"
)

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
		case "string":
			if strings.HasPrefix(t.text, "`") && strings.Contains(t.text, "${") {
				if err := stripTemplateLiteral(t, out); err != nil {
					return i, err
				}
				i++
				continue
			}
			*out = append(*out, t)
			i++
			continue
		case "space", "comment", "number", "regex":
			*out = append(*out, t)
			i++
			continue
		}

		if t.kind == "punct" {
			switch t.text {
			case "(":
				closeIdx := matchParen(tokens, i)
				if closeIdx >= 0 && isArrowParamList(tokens, closeIdx) && (!followsConditionalQuestion(*out) || returnTypedArrowEndsInConditional(tokens, closeIdx)) {
					var err error
					i, err = stripParamList(tokens, i, out, false)
					if err != nil {
						return i, err
					}
					i = stripOptionalReturnType(tokens, i, out)
					continue
				}
				if closeIdx >= 0 && isMethodParamList(tokens, closeIdx, *out) {
					// An object-literal method: `m(a: number): string { ... }`.
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
				// A `!` left in out is always a prefix NOT (a dropped
				// non-null assertion never reaches out), so `!!x` keeps
				// both; a line break before `!` also makes it a prefix.
				if endsExpression(*out) && !endsWithLineBreak(*out) && !closesControlHeader(*out) {
					i++
					// `a!<T>(x)`: the type arguments after a non-null assertion
					// go with it.
					if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "<" {
						if end, ok := consumeCallGenerics(tokens, p); ok && !followedByOptionalChain(tokens, end) {
							for k := i; k < p; k++ {
								*out = append(*out, tokens[k])
							}
							i = end
						}
					}
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
				if last := lastSignificant(*out); last != nil {
					switch {
					case last.kind == "punct" && last.text == "?.":
						// `a?.<T>(x)`: type arguments of an optional call.
						if end, ok := consumeAngleTypeArgsBalanced(tokens, i); ok {
							if n := nextSignificant(tokens, end); n >= 0 && (tokens[n].text == "(" || (tokens[n].kind == "string" && strings.HasPrefix(tokens[n].text, "`"))) {
								i = end
								continue
							}
						}
					case last.kind == "punct" && (last.text == ")" || last.text == "]") && !closesControlHeader(*out):
						// `f(a)<T>(b)`, `a[0]<T>(b)`: type arguments of a call
						// on a call/index result.
						if end, ok := consumeCallGenerics(tokens, i); ok {
							if n := nextSignificant(tokens, end); n >= 0 && (tokens[n].text == "(" || (tokens[n].text == "?." && !keepsTypeArgsBeforeOptionalCall(*out)) || tokens[n].kind == "string") {
								i = end
								continue
							}
						}
					}
				}
				if !endsExpression(*out) {
					// `<T>expr` is a TypeScript angle-bracket assertion:
					// nothing in plain JavaScript starts with `<`, and Node
					// refuses to strip it (ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX).
					if _, ok := consumeAngleTypeArgsBalanced(tokens, i); ok {
						return i, errUnsupportedTSSyntax("angle-bracket type assertion")
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
				if isStatementStart(*out) && nextOnSameLine(tokens, i) {
					pos := t.pos
					i = skipInterfaceDecl(tokens, i)
					asiGuard(tokens, i, out, pos, true)
					continue
				}
			case "type":
				if isStatementStart(*out) && nextOnSameLine(tokens, i) && looksLikeTypeAlias(tokens, i) {
					pos := t.pos
					i = skipTypeAlias(tokens, i)
					asiGuard(tokens, i, out, pos, true)
					continue
				}
			case "satisfies", "as":
				// A line break before `as`/`satisfies` ends the statement
				// (TypeScript allows none before the operator), so the word
				// is an identifier then.
				if endsExpression(*out) && !endsWithLineBreak(*out) {
					// Node erases from the end of the operand, comments
					// before the operator included.
					for n := len(*out); n > 0 && ((*out)[n-1].kind == "space" || (*out)[n-1].kind == "comment"); n-- {
						*out = (*out)[:n-1]
					}
					tokStart := i + 1
					var unterminated bool
					i, unterminated = splitAsType(tokens, i+1)
					if unterminated {
						return i, errUnsupportedTSSyntax("unterminated type")
					}
					if typeStart := nextSignificant(tokens, tokStart); typeStart < 0 || tokens[typeStart].text != "const" {
						// `as const` keeps the following line as is.
						asiGuard(tokens, i, out, t.pos, false)
					}
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
				if nextIdentIs(tokens, i+1, "class") && nextOnSameLine(tokens, i) {
					i = nextSignificant(tokens, i+1)
					continue
				}
			case "declare":
				if isStatementStart(*out) {
					var err error
					pos, before := t.pos, len(*out)
					i, err = handleDeclare(tokens, i, out)
					if err != nil {
						return i, err
					}
					if len(*out) == before {
						asiGuard(tokens, i, out, pos, true)
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
			case "catch":
				// `catch (e: unknown)`: the binding's type annotation is
				// erased.
				*out = append(*out, t)
				i++
				if open := nextSignificant(tokens, i); open >= 0 && tokens[open].kind == "punct" && tokens[open].text == "(" {
					for k := i; k < open; k++ {
						*out = append(*out, tokens[k])
					}
					var err error
					i, err = stripParamList(tokens, open, out, false)
					if err != nil {
						return i, err
					}
				}
				continue
			case "function":
				var err error
				pos, before := t.pos, len(*out)
				i, err = handleFunctionSignature(tokens, i, out)
				if err != nil {
					return i, err
				}
				if len(*out) == before {
					asiGuard(tokens, i, out, pos, true)
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
		afterNew := precededByNew(*out)
		*out = append(*out, t)
		i++
		if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "<" {
			// `new Box<T>;` has type arguments without a call.
			end, ok := consumeCallGenerics(tokens, p)
			if !ok && afterNew {
				end, ok = consumeAngleTypeArgsBalanced(tokens, p)
			}
			if ok {
				if n := nextSignificant(tokens, end); n >= 0 && tokens[n].text == "?." && keepsTypeArgsBeforeOptionalCall(*out) {
					ok = false
				}
			}
			if ok {
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
		// A line break after a complete statement starts the next one
		// (automatic semicolon insertion): `const a = b` newline `type T = ...`.
		return endsWithLineBreak(out) && endsExpression(out)
	}
	switch out[i].text {
	case ";", "}":
		return true
	case "{":
		return looksLikeBlockOpen(out[:i])
	case ")", "]":
		return endsWithLineBreak(out)
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
			case ")", "]", "}":
				return true
			case "++", "--":
				// Postfix when an operand precedes it on the same line
				// (`y++ as T`); otherwise a prefix operator.
				return endsExpression(out[:i]) && !endsWithLineBreak(out[:i])
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

// asiGuard keeps the statement boundary a type-only construct provided. After
// an erased `as T`, type alias, interface, declare statement or overload
// signature, a following line that starts with `(`, `[`, a template, `+`, `-`
// or `/` would otherwise continue the previous expression (`x = y as T`
// newline `(z)` becomes `x = y (z)`). Node writes a `;` over the first erased
// character in that case; pos is that character's rune offset. After an
// expression's `as T` Node does this for `(`, `[` and templates only, because
// a following `+`, `-` or `/` continues the expression as an operator; after a
// statement-level construct (statement is true) it does it for those too.
func asiGuard(tokens []tsToken, next int, out *[]tsToken, pos int, statement bool) {
	sawNewline := false
	j := next
	for ; j < len(tokens) && (tokens[j].kind == "space" || tokens[j].kind == "comment"); j++ {
		if strings.ContainsAny(tokens[j].text, "\n\r") {
			sawNewline = true
		}
	}
	if !sawNewline || j >= len(tokens) {
		return
	}
	t := tokens[j]
	switch {
	case t.kind == "string" && strings.HasPrefix(t.text, "`"):
	case t.kind == "punct" && (t.text == "(" || t.text == "["):
	case statement && t.kind == "regex":
	case statement && t.kind == "punct" && (t.text == "+" || t.text == "-" || t.text == "/"):
	default:
		return
	}
	*out = append(*out, tsToken{kind: "punct", text: ";", pos: pos})
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

// endsWithLineBreak reports whether the output so far ends in a line break
// (ignoring nothing: the whitespace token directly before the current one).
func endsWithLineBreak(out []tsToken) bool {
	if len(out) == 0 {
		return false
	}
	last := out[len(out)-1]
	return (last.kind == "space" || last.kind == "comment") && strings.ContainsAny(last.text, "\n\r")
}

// nextOnSameLine reports whether the next significant token after tokens[i]
// is on the same line: TypeScript reads `type`, `interface` and similar words
// as declarations only when the name follows on the same line.
func nextOnSameLine(tokens []tsToken, i int) bool {
	for k := i + 1; k < len(tokens); k++ {
		switch tokens[k].kind {
		case "space", "comment":
			if strings.ContainsAny(tokens[k].text, "\n\r") {
				return false
			}
		default:
			return true
		}
	}
	return true
}

var methodNameExcluded = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "with": true,
	"await": true, "function": true, "return": true, "typeof": true, "void": true,
	"delete": true, "throw": true, "new": true, "in": true, "of": true, "instanceof": true,
	"yield": true, "case": true, "do": true, "else": true, "extends": true, "super": true,
	"import": true, "export": true, "default": true, "var": true, "let": true, "const": true,
}

// isMethodParamList reports whether the parenthesized span closing at
// tokens[closeIdx] is the parameter list of a method in an object literal:
// a name (not a statement keyword) before the `(` and the body's `{` after
// it, with an optional return type between.
func isMethodParamList(tokens []tsToken, closeIdx int, out []tsToken) bool {
	k := len(out) - 1
	for k >= 0 && (out[k].kind == "space" || out[k].kind == "comment") {
		k--
	}
	if k < 0 {
		return false
	}
	switch out[k].kind {
	case "ident":
		if methodNameExcluded[out[k].text] {
			return false
		}
	case "punct":
		if out[k].text != "]" {
			return false
		}
	case "string", "number":
		// A quoted or numeric method name starts an object member:
		// `{ 'a-b'(x: number) {} }`.
		p := k - 1
		for p >= 0 && (out[p].kind == "space" || out[p].kind == "comment") {
			p--
		}
		if p < 0 || out[p].kind != "punct" || (out[p].text != "{" && out[p].text != ",") {
			return false
		}
	default:
		return false
	}
	next := nextSignificant(tokens, closeIdx+1)
	if next < 0 || tokens[next].kind != "punct" {
		return false
	}
	switch tokens[next].text {
	case "{":
		return true
	case ":":
		end := consumeTypeExpr(tokens, next+1, true)
		if end <= next+1 {
			return false
		}
		brace := nextSignificant(tokens, end)
		return brace >= 0 && tokens[brace].kind == "punct" && tokens[brace].text == "{"
	}
	return false
}

// stripTemplateLiteral emits a template literal token, stripping the
// TypeScript syntax inside its `${...}` expressions (`${x as string}`).
func stripTemplateLiteral(t tsToken, out *[]tsToken) error {
	runes := []rune(t.text)
	n := len(runes)
	emit := func(from, to int) {
		if to > from {
			*out = append(*out, tsToken{"string", string(runes[from:to]), t.pos + from})
		}
	}
	litStart := 0
	for j := 1; j < n; {
		switch {
		case runes[j] == '\\' && j+1 < n:
			j += 2
		case runes[j] == '$' && j+1 < n && runes[j+1] == '{':
			end := scanBalancedExpr(runes, j+2)
			if end > n || end < j+3 || runes[end-1] != '}' {
				j = end
				continue
			}
			emit(litStart, j+2)
			inner := tokenizeTS(string(runes[j+2 : end-1]))
			for k := range inner {
				inner[k].pos += t.pos + j + 2
			}
			var stripped []tsToken
			stop, err := stripBody(inner, 0, &stripped, nil)
			if err != nil {
				return err
			}
			stripped = append(stripped, inner[stop:]...)
			*out = append(*out, stripped...)
			litStart = end - 1
			j = end
		default:
			j++
		}
	}
	emit(litStart, n)
	return nil
}

// closesControlHeader reports whether the last significant token in out is
// the `)` that ends an if/while/for/with/switch header, after which a `!` starts
// the statement as a prefix NOT instead of asserting non-null.
func closesControlHeader(out []tsToken) bool {
	i := len(out) - 1
	for i >= 0 && (out[i].kind == "space" || out[i].kind == "comment") {
		i--
	}
	if i < 0 || out[i].kind != "punct" || out[i].text != ")" {
		return false
	}
	depth := 0
	for ; i >= 0; i-- {
		if out[i].kind != "punct" {
			continue
		}
		switch out[i].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				j := i - 1
				for j >= 0 && (out[j].kind == "space" || out[j].kind == "comment") {
					j--
				}
				if j >= 0 && out[j].kind == "ident" {
					switch out[j].text {
					case "if", "while", "for", "with", "switch", "await":
						return true
					}
				}
				return false
			}
		}
	}
	return false
}

// precededByNew reports whether the last significant token in out is `new`.
func precededByNew(out []tsToken) bool {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].kind == "space" || out[i].kind == "comment" {
			continue
		}
		return out[i].kind == "ident" && out[i].text == "new"
	}
	return false
}

// lastSignificant returns the last emitted token that is not whitespace or a
// comment, or nil.
func lastSignificant(out []tsToken) *tsToken {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].kind != "space" && out[i].kind != "comment" {
			return &out[i]
		}
	}
	return nil
}

// typeSpanUnterminated reports whether a scanned type span opens a bracket it
// never closes (`x as T < y;`, where `<` starts type arguments that run to
// the end of the source). Node rejects that source, so it is not stripped.
func typeSpanUnterminated(span []tsToken) bool {
	depth := 0
	for _, t := range span {
		if t.kind != "punct" {
			continue
		}
		switch t.text {
		case "(", "[", "{", "<":
			depth++
		case ")", "]", "}", ">":
			depth--
		case ">>":
			depth -= 2
		case ">>>":
			depth -= 3
		}
	}
	return depth > 0
}

// keepsTypeArgsBeforeOptionalCall reports whether Node 24 leaves the `<T>` of
// `base<T>?.x` in place: swc does not read type arguments there when base is a
// call result (`f()<T>?.()`, `new A()<T>?.()`) or a member chain that goes
// through `?.` (`a?.b<T>?.()`); it does for `a<T>?.()`, `a.b<T>?.()`,
// `a[0]<T>?.()` and `(a)<T>?.()`.
func keepsTypeArgsBeforeOptionalCall(out []tsToken) bool {
	var sig []tsToken
	for _, t := range out {
		if t.kind != "space" && t.kind != "comment" {
			sig = append(sig, t)
		}
	}
	n := len(sig)
	if n == 0 {
		return false
	}
	last := sig[n-1]
	switch {
	case last.kind == "punct" && last.text == ")":
		open := -1
		depth := 0
		for k := n - 1; k >= 0; k-- {
			if sig[k].kind != "punct" {
				continue
			}
			switch sig[k].text {
			case ")", "]", "}":
				depth++
			case "(", "[", "{":
				depth--
			}
			if depth == 0 {
				open = k
				break
			}
		}
		if open <= 0 {
			return false
		}
		before := sig[open-1]
		if before.kind == "ident" {
			return !isRegexPrecedingKeyword(before.text)
		}
		return before.kind == "punct" && (before.text == ")" || before.text == "]")
	case last.kind == "ident":
		k := n - 1
		for k >= 1 {
			p := sig[k-1]
			if p.kind != "punct" {
				return false
			}
			if p.text == "?." {
				return true
			}
			if p.text != "." || k < 2 {
				return false
			}
			e := k - 2
			if sig[e].kind == "punct" && sig[e].text == "]" {
				depth := 0
				m := e
				for ; m >= 0; m-- {
					if sig[m].kind != "punct" {
						continue
					}
					if sig[m].text == "]" {
						depth++
					} else if sig[m].text == "[" {
						depth--
						if depth == 0 {
							break
						}
					}
				}
				if m < 1 {
					return false
				}
				if sig[m-1].kind == "punct" && sig[m-1].text == "?." {
					return true
				}
				e = m - 1
			}
			if sig[e].kind != "ident" {
				return false
			}
			k = e
		}
	}
	return false
}

// followedByOptionalChain reports whether the next significant token at or
// after i is `?.`. Node keeps `<T>` in `a!<T>?.(x)`.
func followedByOptionalChain(tokens []tsToken, i int) bool {
	n := nextSignificant(tokens, i)
	return n >= 0 && tokens[n].kind == "punct" && tokens[n].text == "?."
}
