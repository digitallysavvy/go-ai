package codemode

// stripOptionalBang drops a `!` (non-null assertion on an expression, or a
// definite-assignment assertion right after a declarator/field name) if
// the next significant token is exactly "!".
func stripOptionalBang(tokens []tsToken, i int, out *[]tsToken) int {
	j := nextSignificant(tokens, i)
	if j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "!" {
		for k := i; k < j; k++ {
			*out = append(*out, tokens[k])
		}
		return j + 1
	}
	return i
}

// stripOptionalMark drops a single-character punctuation marker (`?` or
// `!`) if the next significant token matches it exactly.
func stripOptionalMark(tokens []tsToken, i int, out *[]tsToken, mark string) int {
	j := nextSignificant(tokens, i)
	if j >= 0 && tokens[j].kind == "punct" && tokens[j].text == mark {
		for k := i; k < j; k++ {
			*out = append(*out, tokens[k])
		}
		return j + 1
	}
	return i
}

// stripOptionalTypeAnnotation drops a `: TypeExpr` if the next significant
// token is ":". See consumeTypeExpr for stopAtBrace.
func stripOptionalTypeAnnotation(tokens []tsToken, i int, out *[]tsToken, stopAtBrace bool) int {
	j := nextSignificant(tokens, i)
	if j >= 0 && tokens[j].kind == "punct" && tokens[j].text == ":" {
		for k := i; k < j; k++ {
			*out = append(*out, tokens[k])
		}
		return consumeTypeExpr(tokens, j+1, stopAtBrace)
	}
	return i
}

// stripOptionalReturnType drops a function/method/arrow return type
// annotation (`: TypeExpr`) immediately before its body `{` or its arrow
// `=>`.
func stripOptionalReturnType(tokens []tsToken, i int, out *[]tsToken) int {
	return stripOptionalTypeAnnotation(tokens, i, out, true)
}

// copyBindingTarget copies a binding target -- a plain identifier, or a
// `{...}`/`[...]` destructuring pattern (whose contents are re-scanned
// with stripBody so a default value inside it, e.g. `{ a = (x: number) =>
// x }`, still has its own TypeScript syntax stripped; a rename like
// `{ a: b }` is untouched, since it is copied through as ordinary content,
// never treated as a type annotation). It returns the index just past the
// binding, whether a binding was found, and any error from stripping a
// nested pattern's contents.
func copyBindingTarget(tokens []tsToken, i int, out *[]tsToken) (int, bool, error) {
	j := nextSignificant(tokens, i)
	if j < 0 {
		return i, false, nil
	}
	for k := i; k < j; k++ {
		*out = append(*out, tokens[k])
	}
	t := tokens[j]
	switch {
	case t.kind == "ident":
		*out = append(*out, t)
		return j + 1, true, nil
	case t.kind == "punct" && (t.text == "{" || t.text == "["):
		close := ")"
		if t.text == "{" {
			close = "}"
		} else {
			close = "]"
		}
		*out = append(*out, t)
		end, err := stripBody(tokens, j+1, out, stopAtPunct(close))
		if err != nil {
			return end, true, err
		}
		if end < len(tokens) {
			*out = append(*out, tokens[end])
			end++
		}
		return end, true, nil
	default:
		return j, false, nil
	}
}

// handleDeclaratorList strips binding+type for every comma-separated
// declarator in a `const`/`let`/`var` statement (tokens[i] is right after
// the keyword), recursing into each initializer expression with stripBody
// so nested TypeScript syntax (an arrow function with typed parameters, an
// `as` assertion, ...) is still handled.
func handleDeclaratorList(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	for {
		next, ok, err := copyBindingTarget(tokens, i, out)
		if err != nil {
			return next, err
		}
		if !ok {
			return next, nil
		}
		i = next
		i = stripOptionalBang(tokens, i, out)
		i = stripOptionalTypeAnnotation(tokens, i, out, false)
		if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "=" {
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
			i, err = stripBody(tokens, i, out, stopAtAny(",", ";"))
			if err != nil {
				return i, err
			}
		}
		if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "," {
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
			continue
		}
		return i, nil
	}
}

// stripParamList strips a parenthesized parameter list starting at
// tokens[openIdx] (the "("): type annotations, optional `?` markers,
// access-modifier keywords on constructor parameters (reported as an
// errUnsupportedTSSyntax error -- a parameter property auto-assigns
// `this.x = x`, so merely erasing the modifier would silently drop that
// behavior, unlike every other erasure in this package), and destructuring
// patterns/default values, which are recursively stripped via stripBody.
func stripParamList(tokens []tsToken, openIdx int, out *[]tsToken, isConstructor bool) (int, error) {
	*out = append(*out, tokens[openIdx])
	i := openIdx + 1
	for {
		j := nextSignificant(tokens, i)
		if j < 0 {
			for k := i; k < len(tokens); k++ {
				*out = append(*out, tokens[k])
			}
			return len(tokens), nil
		}
		if tokens[j].kind == "punct" && tokens[j].text == ")" {
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			return j + 1, nil
		}
		next, err := stripOneParam(tokens, i, out, isConstructor)
		if err != nil {
			return next, err
		}
		if next == i {
			// No progress (unrecognized token where a parameter was
			// expected): copy it through as-is to guarantee forward
			// progress and avoid an infinite loop on malformed input.
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			next = j + 1
		}
		i = next
		if comma := nextSignificant(tokens, i); comma >= 0 && tokens[comma].kind == "punct" && tokens[comma].text == "," {
			for k := i; k <= comma; k++ {
				*out = append(*out, tokens[k])
			}
			i = comma + 1
		}
	}
}

var paramAccessModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "readonly": true, "override": true,
}

// stripOneParam strips one parameter's type annotation/default value.
func stripOneParam(tokens []tsToken, i int, out *[]tsToken, isConstructor bool) (int, error) {
	j := nextSignificant(tokens, i)
	if j < 0 {
		return i, nil
	}

	// A TypeScript `this` parameter (the parameter must be literally named
	// "this") types the function's `this` context and has no runtime
	// representation -- unlike every other parameter, even its *name* must
	// be erased along with its type annotation and the comma that followed
	// it: `this` can never legally be a JavaScript parameter name (it's a
	// reserved word), so leaving it in place the way an ordinary typed
	// parameter's name survives would hand the engine invalid syntax
	// instead of an erased no-op, unlike every other erasure in this
	// package. It can't carry a rest marker, access modifier, "?", or
	// default value (TypeScript's grammar disallows all of those on a
	// `this` parameter), so this check runs before any of that handling.
	// Verified directly against Node's stripTypeScriptTypes -- see
	// strip_types_test.go.
	if tokens[j].kind == "ident" && tokens[j].text == "this" {
		for k := i; k < j; k++ {
			*out = append(*out, tokens[k])
		}
		end := j + 1
		if c := nextSignificant(tokens, end); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == ":" {
			end = consumeTypeExpr(tokens, c+1, false)
		}
		if comma := nextSignificant(tokens, end); comma >= 0 && tokens[comma].kind == "punct" && tokens[comma].text == "," {
			end = comma + 1
		}
		return end, nil
	}

	for k := i; k < j; k++ {
		*out = append(*out, tokens[k])
	}
	i = j

	// Rest parameter marker.
	if tokens[i].kind == "punct" && tokens[i].text == "..." {
		*out = append(*out, tokens[i])
		i++
		if j := nextSignificant(tokens, i); j >= 0 {
			for k := i; k < j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j
		}
	}

	// Access-modifier keywords: only valid (and only erasable/erroring) on
	// a constructor parameter property.
	sawModifier := false
	for {
		k := nextSignificant(tokens, i)
		if k < 0 || tokens[k].kind != "ident" || !paramAccessModifiers[tokens[k].text] {
			break
		}
		sawModifier = true
		i = k + 1
	}
	if sawModifier {
		if isConstructor {
			return i, errUnsupportedTSSyntax("constructor parameter property")
		}
		if j := nextSignificant(tokens, i); j >= 0 {
			for k := i; k < j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j
		}
	}

	next, ok, err := copyBindingTarget(tokens, i, out)
	if err != nil {
		return next, err
	}
	if !ok {
		return i, nil
	}
	i = next

	i = stripOptionalMark(tokens, i, out, "?")
	i = stripOptionalTypeAnnotation(tokens, i, out, false)

	if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "=" {
		for k := i; k <= j; k++ {
			*out = append(*out, tokens[k])
		}
		i = j + 1
		i, err = stripBody(tokens, i, out, stopAtAny(",", ")"))
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

// handleFunctionSignature strips a function declaration/expression's
// generics, parameter types, and return type. tokens[i] is "function". If
// the signature has a body (`{`), control returns right at the `{`, for
// the caller (stripBody) to recurse into normally. If it doesn't (a
// bodyless overload signature or an ambient declaration reached this way),
// the whole signature -- including its trailing `;` -- is erased, matching
// Node's stripTypeScriptTypes exactly (see strip_types_test.go).
func handleFunctionSignature(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	memberStart := len(*out)
	*out = append(*out, tokens[i])
	i++

	if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "punct" && tokens[j].text == "*" {
		for k := i; k <= j; k++ {
			*out = append(*out, tokens[k])
		}
		i = j + 1
	}
	if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "ident" {
		for k := i; k <= j; k++ {
			*out = append(*out, tokens[k])
		}
		i = j + 1
	}
	i = stripOptionalTypeParams(tokens, i, out)

	open := nextSignificant(tokens, i)
	if open < 0 || !(tokens[open].kind == "punct" && tokens[open].text == "(") {
		return i, nil
	}
	for k := i; k < open; k++ {
		*out = append(*out, tokens[k])
	}
	var err error
	i, err = stripParamList(tokens, open, out, false)
	if err != nil {
		return i, err
	}
	i = stripOptionalReturnType(tokens, i, out)

	next := nextSignificant(tokens, i)
	if next >= 0 && tokens[next].kind == "punct" && tokens[next].text == "{" {
		return i, nil
	}
	end := scanToStatementEnd(tokens, i)
	*out = (*out)[:memberStart]
	return end, nil
}
