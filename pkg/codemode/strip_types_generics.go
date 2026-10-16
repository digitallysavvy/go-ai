package codemode

import "strings"

// consumeTypeExpr skips a type expression starting at tokens[i] and trims
// any trailing whitespace/comments off the end of the dropped span, so
// formatting between the type and whatever follows it (e.g. the space
// before a function body's "{") is preserved rather than silently
// absorbed into the deletion. See consumeTypeExprRaw for the scan itself.
func consumeTypeExpr(tokens []tsToken, i int, stopAtBrace bool) int {
	end := consumeTypeExprRaw(tokens, i, stopAtBrace)
	for end > i && (tokens[end-1].kind == "space" || tokens[end-1].kind == "comment") {
		end--
	}
	return end
}

// consumeTypeExprRaw skips a type expression starting at tokens[i], balancing
// (), [], {} and <> (including the merged ">>"/">>>" shift-operator tokens,
// re-interpreted as closing two/three nested angle brackets at once, the
// way real TypeScript parsers re-lex them) and stopping at the first
// top-level `;`, `,`, `)`, `}`, `]`, `=`, or `=>`. When stopAtBrace is true,
// a top-level `{` after a complete type also stops the scan without being
// consumed (used for a function/method return type, where `{` starts the
// body); a `{` where an operand is expected is an object type literal in
// either mode (`(): { a: number } => ...`).
//
// The scan follows the TypeScript grammar where it matters for where a type
// ends:
//   - A parenthesized (or generic) group at the start of an operand that is
//     followed by `=>` is a function type, and the `=>` belongs to it
//     (`let f: (a: number) => void`, `new () => T`); any other top-level
//     `=>` ends the type (an arrow function's return type).
//   - `?` at the top level opens a conditional type, whose `:` belongs to
//     it; any other top-level `:` ends the type (`case x as T: ...`,
//     `c ? v as T : w`).
//   - A complete type ends at a line break unless the next token continues
//     it (`|`, `&`, `.`, `extends`, ...), because TypeScript ends a type
//     there (`let x: number` newline `let y = 2`).
func consumeTypeExprRaw(tokens []tsToken, i int, stopAtBrace bool) int {
	end, _ := consumeTypeExprScan(tokens, i, stopAtBrace, false)
	return end
}

// splitAsType is consumeTypeExpr for the type after `as`/`satisfies`. When
// the type's closing `>` is merged into a longer operator token (`y as
// A<B>>1`, `>=`, `>>=`), TypeScript re-lexes it: the type ends inside the
// token and the rest (`>1`, `=`) is an operator again. The token is replaced
// by that rest so the caller keeps it; the rest of the token is blanked with
// the type. It mutates tokens, so only a non-speculative caller may use it.
func splitAsType(tokens []tsToken, i int) (int, bool) {
	end, split := consumeTypeExprScan(tokens, i, false, true)
	if split.idx >= 0 {
		t := tokens[split.idx]
		tokens[split.idx] = tsToken{kind: "punct", text: t.text[split.take:], pos: t.pos + split.take}
		end = split.idx
	}
	unterminated := split.idx < 0 && typeSpanUnterminated(tokens[i:end])
	for end > i && (tokens[end-1].kind == "space" || tokens[end-1].kind == "comment") {
		end--
	}
	return end, unterminated
}

type typeSplit struct{ idx, take int }

func consumeTypeExprScan(tokens []tsToken, i int, stopAtBrace, allowSplit bool) (int, typeSplit) {
	noSplit := typeSplit{idx: -1}
	depth := 0
	cond := 0
	operandStart := true
	fnCandidate := false
	genericCandidate := false
	extendsOpen := 0
	var prev *tsToken
	sawNewline := false
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == "space" || t.kind == "comment" {
			if t.kind == "space" && strings.Contains(t.text, "\n") {
				sawNewline = true
			}
			i++
			continue
		}
		if depth == 0 && sawNewline && prev != nil && typeOperandEnds(prev) && !typeContinuesAfterNewline(t) {
			return i, noSplit
		}
		sawNewline = false
		if depth == 0 && prev != nil && typeOperandEnds(prev) && endsTypeBeforeOperator(t, prev, extendsOpen) {
			return i, noSplit
		}
		// A following `as`/`satisfies` is the next assertion of a chain
		// (`y as unknown as T`), not part of this type.
		if depth == 0 && prev != nil && typeOperandEnds(prev) && t.kind == "ident" && (t.text == "as" || t.text == "satisfies") {
			return i, noSplit
		}
		if depth == 0 && t.kind == "ident" && t.text == "extends" {
			extendsOpen++
		}
		if t.kind == "punct" {
			switch t.text {
			case "(", "[":
				if depth == 0 && t.text == "(" && (operandStart || genericCandidate) {
					fnCandidate = isFunctionTypeParams(tokens, i)
				}
				depth++
			case ")", "]":
				if depth == 0 {
					return i, noSplit
				}
				depth--
				if depth == 0 && t.text == ")" && fnCandidate {
					fnCandidate = false
					genericCandidate = false
					if n := nextSignificant(tokens, i+1); n >= 0 && tokens[n].kind == "punct" && tokens[n].text == "=>" {
						// A function type: its `=>` and return type follow.
						i = n + 1
						prev = &tokens[n]
						operandStart = true
						continue
					}
				}
			case "{":
				if stopAtBrace && depth == 0 && !operandStart {
					return i, noSplit
				}
				depth++
			case "}":
				if depth == 0 {
					return i, noSplit
				}
				depth--
			case "<":
				if depth == 0 && operandStart {
					genericCandidate = true
				}
				depth++
			case ">":
				if depth > 0 {
					depth--
				}
			case ">>", ">>>", ">=", ">>=", ">>>=":
				if !allowSplit {
					switch t.text {
					case ">>":
						depth = max(depth-2, 0)
					case ">>>":
						depth = max(depth-3, 0)
					}
				} else if depth > 0 {
					k := len(t.text) - len(strings.TrimLeft(t.text, ">"))
					if depth < k || (depth == k && k < len(t.text)) {
						return i, typeSplit{idx: i, take: depth}
					}
					if depth == k {
						depth = 0
					} else {
						depth -= k
					}
				} else if t.text == ">>" || t.text == ">>>" {
					depth = 0
				}
			case "?":
				if depth == 0 {
					cond++
					if extendsOpen > 0 {
						extendsOpen--
					}
				}
			case ":":
				if depth == 0 {
					if cond == 0 {
						return i, noSplit
					}
					cond--
				}
			case ";", ",", "=", "=>", "!":
				// A `!` has no place in a type: at the top level the scan has
				// run into an expression (`c ? (x) : y(y)! satisfies T`).
				if depth == 0 {
					return i, noSplit
				}
			}
		}
		if depth == 0 {
			operandStart = typeOperandStarts(t)
		}
		prev = &tokens[i]
		i++
	}
	return i, noSplit
}

// binaryOperatorTokens are the operators that can follow a complete type at
// the top level only because the type ended there: `a as number + 1`, `x as T
// === y`. A type never continues into one of them.
var binaryOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "**": true,
	"==": true, "===": true, "!=": true, "!==": true, "<=": true, ">=": true,
	">": true, ">>": true, ">>>": true, "<<": true, "&&": true, "||": true,
	"??": true, "^": true,
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, ">>>=": true,
	"&&=": true, "||=": true, "??=": true,
}

// primitiveTypeKeywords are type names that can never take type arguments,
// so a `<` after one is a less-than comparison.
var primitiveTypeKeywords = map[string]bool{
	"number": true, "string": true, "boolean": true, "any": true, "unknown": true,
	"never": true, "void": true, "null": true, "undefined": true, "object": true,
	"symbol": true, "bigint": true, "true": true, "false": true, "this": true,
}

// endsTypeBeforeOperator reports whether t, right after the complete type
// operand prev at the top level, ends the type because it is an expression
// operator (`as number + 1`, `as T in obj`, `as T ? a : b`). extendsOpen
// counts `extends` keywords whose conditional `?` has not been seen yet: only
// those make a `?` part of the type.
func endsTypeBeforeOperator(t tsToken, prev *tsToken, extendsOpen int) bool {
	switch t.kind {
	case "punct":
		switch {
		case binaryOperatorTokens[t.text]:
			return true
		case t.text == "?":
			return extendsOpen == 0
		case t.text == "<":
			// Only a type name takes type arguments: after a literal, a
			// primitive keyword or the end of a bracketed or generic type
			// (`as Array<T> < c`, `as { k: T } < c`) the `<` is a comparison.
			if prev.kind == "punct" {
				switch prev.text {
				case ">", ">>", ">>>", "]", "}", ")":
					return true
				}
			}
			return prev.kind == "string" || prev.kind == "number" || (prev.kind == "ident" && primitiveTypeKeywords[prev.text])
		}
	case "ident":
		return t.text == "in" || t.text == "instanceof"
	}
	return false
}

// typeOperandStarts reports whether a type operand begins right after t at
// the top level of a type: after an operator that takes a right-hand type,
// or a keyword that prefixes one.
func typeOperandStarts(t tsToken) bool {
	switch t.kind {
	case "punct":
		switch t.text {
		case "|", "&", "?", ":", "=>", "<", "(", "[", ",":
			return true
		}
	case "ident":
		switch t.text {
		case "extends", "keyof", "typeof", "readonly", "unique", "infer", "new", "abstract", "asserts", "is", "in", "out":
			return true
		}
	}
	return false
}

// typeOperandEnds reports whether a type can end right after t.
func typeOperandEnds(t *tsToken) bool {
	switch t.kind {
	case "ident":
		return !typeOperandStarts(*t)
	case "string", "number", "regex":
		return true
	case "punct":
		switch t.text {
		case ")", "]", ">", ">>", ">>>", "}":
			return true
		}
	}
	return false
}

// typeContinuesAfterNewline reports whether t, on a new line after a
// complete type, still belongs to that type.
func typeContinuesAfterNewline(t tsToken) bool {
	switch t.kind {
	case "punct":
		switch t.text {
		case "|", "&", ".", "=>", "?", ":", ")", "]", "}", ",", ";", "=", ">":
			return true
		}
	case "ident":
		switch t.text {
		case "extends", "is":
			return true
		}
	}
	return false
}

// angleDisallowedAtDepth0 lists operator tokens that can never appear
// directly inside a `<...>` type-argument/type-parameter list (as opposed
// to nested inside a `()`/`[]`/`{}` sub-expression, e.g. an object type's
// members). Seeing one disqualifies a leading `<` from being generics --
// it's an ordinary less-than comparison instead. This is the key to
// resolving `a < b > c` (rejected: "c" follows with no disallowed token,
// but the character after the close isn't "(" -- see consumeCallGenerics)
// against `f<number>(1)` and `a < f<number>(b)` (accepted).
//
// A bare "=" is deliberately *not* in this set: at this nesting depth it can
// only be a type parameter's default (`<T = number>`, `class C<T = number>`,
// `<T = number>(x: T) => x`), never a real comparison -- a genuine `a < b =
// c` isn't valid JavaScript without parentheses around `b = c` (assignment
// binds looser than comparison), and parenthesizing it would put the "="
// inside a nested "(" that already bumps the depth past 0. Verified
// directly against Node's stripTypeScriptTypes -- see strip_types_test.go.
var angleDisallowedAtDepth0 = map[string]bool{
	";": true, "&&": true, "||": true, "??": true,
	"++": true, "--": true, "==": true, "===": true, "!=": true, "!==": true,
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, ">>>=": true,
	"&&=": true, "||=": true, "??=": true, "*": true, "/": true, "%": true,
}

// consumeAngleTypeArgsBalanced attempts to consume a `<...>`
// type-argument/type-parameter list starting at tokens[i] (the "<"),
// requiring it to be structurally balanced and free of tokens that could
// never appear in a type position. It returns the index just past the
// matching ">" (or the corresponding share of a merged ">>"/">>>" token)
// and true on success, or (i, false) if this doesn't look like a type
// argument list.
func consumeAngleTypeArgsBalanced(tokens []tsToken, i int) (int, bool) {
	if tokens[i].kind != "punct" || tokens[i].text != "<" {
		return i, false
	}
	depth := 1
	contentSeen := false
	sawExtends := false
	var prev *tsToken
	j := i + 1
	for j < len(tokens) {
		t := tokens[j]
		if t.kind != "punct" {
			if t.kind != "space" && t.kind != "comment" {
				contentSeen = true
				if depth == 1 && t.kind == "ident" && t.text == "extends" {
					sawExtends = true
				}
				// Two operands in a row (`a < b` newline `y = c > (d)`) are
				// not a type list: a type cannot be followed by a bare name.
				if depth == 1 && prev != nil && prev.text != "const" && typeOperandEnds(prev) && !typeContinuesAfterNewline(t) &&
					(t.kind == "ident" || t.kind == "string" || t.kind == "number") && !typeOperandStarts(t) {
					return i, false
				}
				prev = &tokens[j]
			}
			j++
			continue
		}
		prev = &tokens[j]
		switch t.text {
		case "<":
			depth++
			contentSeen = true
			j++
		case ">":
			depth--
			j++
			if depth == 0 {
				if !contentSeen {
					return i, false
				}
				return j, true
			}
			if depth < 0 {
				return i, false
			}
		case ">>":
			if depth < 2 {
				return i, false
			}
			depth -= 2
			j++
			if depth == 0 {
				return j, true
			}
		case ">>>":
			if depth < 3 {
				return i, false
			}
			depth -= 3
			j++
			if depth == 0 {
				return j, true
			}
		case "(", "[", "{":
			depth++
			contentSeen = true
			j++
		case ")", "]", "}":
			depth--
			if depth < 1 {
				return i, false
			}
			j++
		default:
			if depth == 1 && angleDisallowedAtDepth0[t.text] {
				return i, false
			}
			// A `?` directly inside the list is a conditional type's, which
			// needs its `extends`; `a < b ? c : d > (e)` compares.
			if depth == 1 && t.text == "?" && !sawExtends {
				return i, false
			}
			contentSeen = true
			j++
		}
	}
	return i, false
}

// consumeCallGenerics is consumeAngleTypeArgsBalanced plus the
// disambiguating rule for an expression-position `<...>` (a call's type
// arguments, or a generic arrow function's type parameters): the token
// immediately after the matching close must be "(", which is the only
// grammatically valid continuation for either. Without this extra check, a
// bare comparison chain like `a < b > c` would otherwise parse as balanced
// "generics" (`<b>`) followed by an unrelated `c`.
func consumeCallGenerics(tokens []tsToken, i int) (int, bool) {
	end, ok := consumeAngleTypeArgsBalanced(tokens, i)
	if !ok {
		return i, false
	}
	// The tokenizer lexes `>=` as `>` and `=`. A `>` directly followed by `=`
	// is the comparison operator (`a < b >= c`), not the end of a type
	// argument list.
	if end < len(tokens) && tokens[end-1].text == ">" && tokens[end].kind == "punct" && tokens[end].text == "=" {
		return i, false
	}
	next := nextSignificant(tokens, end)
	if next >= 0 && tokens[next].kind == "punct" && tokens[next].text == "(" {
		return end, true
	}
	// A tagged template with type arguments: tag<T>`text`.
	if next >= 0 && tokens[next].kind == "string" && strings.HasPrefix(tokens[next].text, "`") {
		return end, true
	}
	// An instantiation expression (`const f = id<number>;`): TS
	// canFollowTypeArgumentsInExpression accepts the type arguments when the
	// next token cannot continue a comparison.
	if canFollowTypeArgsInExpression(tokens, end) {
		return end, true
	}
	return i, false
}

// canFollowTypeArgsInExpression mirrors the TypeScript parser's rule for
// `expr<T>` without a call: the type arguments stand when a line break
// follows, when the next token is a binary operator, or when it cannot start
// an expression. `<`, `>`, `+` and `-` never qualify.
func canFollowTypeArgsInExpression(tokens []tsToken, end int) bool {
	lineBreak := false
	p := end
	for p < len(tokens) && (tokens[p].kind == "space" || tokens[p].kind == "comment") {
		if tokens[p].kind == "space" && strings.Contains(tokens[p].text, "\n") {
			lineBreak = true
		}
		p++
	}
	if p >= len(tokens) {
		return true
	}
	t := tokens[p]
	switch t.kind {
	case "ident":
		if lineBreak {
			return true
		}
		switch t.text {
		case "in", "instanceof", "as", "satisfies", "of":
			return true
		}
		return false
	case "number", "regex":
		return lineBreak
	case "string":
		return lineBreak || strings.HasPrefix(t.text, "`")
	}
	switch t.text {
	case "<", ">", "+", "-", ">>", ">>>", ">=", ">>=", ">>>=":
		return false
	}
	if lineBreak {
		return true
	}
	switch t.text {
	case "(", "[", "{", "!", "~", "++", "--", "@", "#", "...":
		return t.text == "("
	}
	return true
}

// stripOptionalTypeParams drops a declaration's own `<...>` type parameter
// list (e.g. `function f<T>`, `class C<T>`), which -- unlike a call's or a
// generic arrow's type arguments -- is never ambiguous with a comparison:
// it can only be generics in this grammatical position, so no "must be
// followed by (" check is needed.
func stripOptionalTypeParams(tokens []tsToken, i int, out *[]tsToken) int {
	j := nextSignificant(tokens, i)
	if j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "<" {
		if end, ok := consumeAngleTypeArgsBalanced(tokens, j); ok {
			for k := i; k < j; k++ {
				*out = append(*out, tokens[k])
			}
			return end
		}
	}
	return i
}

// matchParen returns the index of the bracket matching tokens[openIdx]
// (one of "(", "[", "{"), balancing all three bracket kinds together, or
// -1 if unbalanced.
func matchParen(tokens []tsToken, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(tokens); i++ {
		if tokens[i].kind != "punct" {
			continue
		}
		switch tokens[i].text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isArrowParamList reports whether the parenthesized span closing at
// tokens[closeParenIdx] is followed by "=>" (directly, or after a `:
// ReturnType` annotation), which is how an arrow function's parameter list
// is distinguished from an ordinary parenthesized expression or call
// argument list.
func isArrowParamList(tokens []tsToken, closeParenIdx int) bool {
	next := nextSignificant(tokens, closeParenIdx+1)
	if next < 0 {
		return false
	}
	if tokens[next].kind == "punct" && tokens[next].text == "=>" {
		return true
	}
	if tokens[next].kind == "punct" && tokens[next].text == ":" {
		end := consumeTypeExpr(tokens, next+1, true)
		arrow := nextSignificant(tokens, end)
		return arrow >= 0 && tokens[arrow].kind == "punct" && tokens[arrow].text == "=>"
	}
	return false
}

// tryGenericArrowTypeParams attempts to parse tokens[i] (a "<") as a generic
// arrow function's own type parameter list -- `<T,>(x: T) => x`,
// `<T extends U>(x: T): T => x` -- the same construct Node's
// stripTypeScriptTypes recognizes when parsing a `.ts` source file. Unlike
// `.tsx`, a `.ts` file has no JSX, so `<T>(x) => x` is unambiguous without a
// trailing comma after "T" (verified directly against Node -- see
// strip_types_test.go); code-mode snippets are always parsed as `.ts`, so
// this scanner doesn't require one either.
//
// It requires: a balanced, non-empty `<...>` that cannot be a comparison
// chain (see consumeAngleTypeArgsBalanced), followed directly by a
// parenthesized parameter list that is itself an arrow function's parameter
// list (isArrowParamList: followed by "=>", or by a `: ReturnType =>`). This
// is exactly the same "must be followed by (" plus "the ( must be an arrow
// param list" pair of checks consumeCallGenerics and isArrowParamList apply
// for a call's type arguments; without both, `a < b` followed unrelatedly by
// a parenthesized expression could be misread as generics. It returns the
// index of the "(" that begins the parameter list and true on success, or
// (i, false) if tokens[i] doesn't begin one.
func tryGenericArrowTypeParams(tokens []tsToken, i int) (int, bool) {
	end, ok := consumeAngleTypeArgsBalanced(tokens, i)
	if !ok {
		return i, false
	}
	open := nextSignificant(tokens, end)
	if open < 0 || tokens[open].kind != "punct" || tokens[open].text != "(" {
		return i, false
	}
	closeIdx := matchParen(tokens, open)
	if closeIdx < 0 || !isArrowParamList(tokens, closeIdx) {
		return i, false
	}
	return open, true
}

// scanUntilTopLevelBrace returns the index of the first "{" at bracket
// depth 0 relative to i (balancing "(){}[]<>" along the way), or
// len(tokens) if none is found. It's used to find the end of a class's
// `implements A<T>, B` clause, which is dropped entirely.
func scanUntilTopLevelBrace(tokens []tsToken, i int) int {
	depth := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == "punct" {
			switch t.text {
			case "(", "[", "<":
				depth++
			case ")", "]", ">":
				if depth > 0 {
					depth--
				}
			case ">>":
				if depth >= 2 {
					depth -= 2
				} else {
					depth = 0
				}
			case ">>>":
				if depth >= 3 {
					depth -= 3
				} else {
					depth = 0
				}
			case "{":
				if depth == 0 {
					return i
				}
				depth++
			case "}":
				if depth > 0 {
					depth--
				}
			}
		}
		i++
	}
	return i
}

// returnTypedArrowEndsInConditional reports whether the arrow function whose
// parameter list closes at tokens[closeIdx], written with a `: ReturnType`,
// may stand as the true branch of a conditional. TypeScript only accepts it
// there when the arrow body is followed by the `:` of the conditional;
// otherwise `c ? (x) : y => z` is a conditional whose false branch is the
// arrow `y => z`.
func returnTypedArrowEndsInConditional(tokens []tsToken, closeIdx int) bool {
	colon := nextSignificant(tokens, closeIdx+1)
	if colon < 0 || tokens[colon].text != ":" {
		return true
	}
	end := consumeTypeExpr(tokens, colon+1, true)
	arrow := nextSignificant(tokens, end)
	if arrow < 0 || tokens[arrow].text != "=>" {
		return true
	}
	j := nextSignificant(tokens, arrow+1)
	if j < 0 {
		return false
	}
	if tokens[j].kind == "punct" && tokens[j].text == "{" {
		m := matchParen(tokens, j)
		if m < 0 {
			return false
		}
		n := nextSignificant(tokens, m+1)
		return n >= 0 && tokens[n].kind == "punct" && tokens[n].text == ":"
	}
	depth, pending := 0, 0
	for ; j < len(tokens); j++ {
		t := tokens[j]
		if t.kind == "space" && depth == 0 && strings.ContainsAny(t.text, "\n\r\u2028\u2029") && asiBreaksBefore(tokens, j) {
			// The line break ends the arrow body (ASI): a `:` further down
			// belongs to another statement.
			return false
		}
		if t.kind == "punct" && t.text == "<" && depth == 0 {
			// Type arguments (`f<A, B>(x)`, `as Map<K, V>`): their commas are
			// not the body's.
			if end, ok := consumeCallGenerics(tokens, j); ok {
				j = end - 1
				continue
			}
		}
		if t.kind != "punct" {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return false
			}
			depth--
		case ";", ",":
			if depth == 0 {
				return false
			}
		case "?":
			if depth == 0 {
				pending++
			}
		case ":":
			if depth == 0 {
				if pending == 0 {
					return true
				}
				pending--
			}
		}
	}
	return false
}

// asiBreaksBefore reports whether the line break at tokens[j] ends a
// statement by automatic semicolon insertion: the token before it completes an
// operand and the next one starts a new operand (an identifier, number or
// string) rather than continuing the expression with an operator, call, index
// or template.
func asiBreaksBefore(tokens []tsToken, j int) bool {
	prev := -1
	for k := j - 1; k >= 0; k-- {
		if tokens[k].kind != "space" && tokens[k].kind != "comment" {
			prev = k
			break
		}
	}
	next := nextSignificant(tokens, j+1)
	if prev < 0 || next < 0 {
		return false
	}
	p, n := tokens[prev], tokens[next]
	switch p.kind {
	case "number", "string", "regex":
	case "ident":
		switch p.text {
		case "typeof", "void", "delete", "new", "await", "yield", "in", "of", "instanceof", "as", "satisfies", "extends", "return", "throw", "case", "else", "do":
			return false
		}
	case "punct":
		if p.text != ")" && p.text != "]" && p.text != "}" && p.text != "++" && p.text != "--" && p.text != "!" {
			return false
		}
	default:
		return false
	}
	switch n.kind {
	case "number":
		return true
	case "ident":
		switch n.text {
		case "in", "of", "instanceof", "as", "satisfies", "extends":
			return false
		}
		return true
	}
	return false
}

// inCaseLabel reports whether the emitted tokens end inside a `case` clause's
// expression: a `case` before the end, at the same bracket level, with no `:`
// or statement boundary in between. The `:` after such an expression closes
// the label, so `case (x): y => 1` holds an arrow statement, not a return-typed
// arrow.
func inCaseLabel(out []tsToken) bool {
	for i := len(out) - 1; i >= 0; i-- {
		t := out[i]
		switch {
		case t.kind == "ident" && t.text == "case":
			return true
		case t.kind != "punct":
			continue
		}
		switch t.text {
		case ")", "]", "}":
			depth := 1
			for i--; i >= 0 && depth > 0; i-- {
				if out[i].kind != "punct" {
					continue
				}
				switch out[i].text {
				case ")", "]", "}":
					depth++
				case "(", "[", "{":
					depth--
				}
			}
			i++
		case "(", "[", "{", ";", ":":
			return false
		}
	}
	return false
}

// inConditionalTrueBranch reports whether the emitted tokens end inside the
// true branch of a conditional expression: an unmatched `?` before the end,
// at the same bracket level and within the same statement.
func inConditionalTrueBranch(out []tsToken) bool {
	pending := 0
	for i := len(out) - 1; i >= 0; i-- {
		t := out[i]
		if t.kind != "punct" {
			continue
		}
		switch t.text {
		case ")", "]", "}":
			depth := 1
			for i--; i >= 0 && depth > 0; i-- {
				if out[i].kind != "punct" {
					continue
				}
				switch out[i].text {
				case ")", "]", "}":
					depth++
				case "(", "[", "{":
					depth--
				}
			}
			i++
		case "(", "[", "{", ";":
			return false
		case ":":
			pending++
		case "?":
			if pending == 0 {
				return true
			}
			pending--
		}
	}
	return false
}

// isFunctionTypeParams reports whether the "(" at tokens[open], where a type
// operand begins, can start the parameter list of a function type: `()`, a
// rest parameter, or a first parameter (a name or a destructuring pattern)
// followed by `:`, `,`, `?`, `=` or the closing `)`. Anything else, such as
// `(A | B)`, `(() => void)` or `(new () => A)`, is a parenthesized type, and
// a following `=>` then belongs to an arrow function (`(a): (() => void) =>
// a`), not to the type.
func isFunctionTypeParams(tokens []tsToken, open int) bool {
	j := nextSignificant(tokens, open+1)
	if j < 0 {
		return false
	}
	t := tokens[j]
	if t.kind == "punct" {
		switch t.text {
		case ")", "...":
			return true
		case "{", "[":
			if m := matchParen(tokens, j); m >= 0 {
				j = m
			} else {
				return false
			}
		default:
			return false
		}
	} else if t.kind != "ident" {
		return false
	} else if t.text == "new" || t.text == "typeof" || t.text == "keyof" || t.text == "readonly" || t.text == "unique" || t.text == "infer" || t.text == "asserts" {
		if n := nextSignificant(tokens, j+1); n >= 0 && tokens[n].kind == "punct" && (tokens[n].text == ":" || tokens[n].text == "," || tokens[n].text == ")" || tokens[n].text == "?" || tokens[n].text == "=") {
			return true
		}
		return false
	}
	n := nextSignificant(tokens, j+1)
	if n < 0 || tokens[n].kind != "punct" {
		return false
	}
	switch tokens[n].text {
	case ":", ",", "?", "=", ")":
		return true
	}
	return false
}
