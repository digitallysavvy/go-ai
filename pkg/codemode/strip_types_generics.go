package codemode

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
// a top-level `{` also stops the scan without being consumed (used for a
// function/method/arrow return type, where `{` starts the body rather than
// an object type literal); when false, `{` opens a nested object type
// literal to balance instead (used for variable/parameter/field type
// annotations and `as`/`satisfies` assertions, where `: { a: number }` is
// valid content).
func consumeTypeExprRaw(tokens []tsToken, i int, stopAtBrace bool) int {
	depth := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == "punct" {
			switch t.text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth == 0 {
					return i
				}
				depth--
			case "{":
				if stopAtBrace && depth == 0 {
					return i
				}
				depth++
			case "}":
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

// angleDisallowedAtDepth0 lists operator tokens that can never appear
// directly inside a `<...>` type-argument/type-parameter list (as opposed
// to nested inside a `()`/`[]`/`{}` sub-expression, e.g. an object type's
// members). Seeing one disqualifies a leading `<` from being generics --
// it's an ordinary less-than comparison instead. This is the key to
// resolving `a < b > c` (rejected: "c" follows with no disallowed token,
// but the character after the close isn't "(" -- see consumeCallGenerics)
// against `f<number>(1)` and `a < f<number>(b)` (accepted).
var angleDisallowedAtDepth0 = map[string]bool{
	";": true, "=": true, "&&": true, "||": true, "??": true,
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
	if !(tokens[i].kind == "punct" && tokens[i].text == "<") {
		return i, false
	}
	depth := 1
	contentSeen := false
	j := i + 1
	for j < len(tokens) {
		t := tokens[j]
		if t.kind != "punct" {
			if t.kind != "space" && t.kind != "comment" {
				contentSeen = true
			}
			j++
			continue
		}
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
	next := nextSignificant(tokens, end)
	if next >= 0 && tokens[next].kind == "punct" && tokens[next].text == "(" {
		return end, true
	}
	return i, false
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
