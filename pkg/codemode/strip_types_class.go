package codemode

// handleClassDecl strips a class declaration/expression's generics,
// `extends`/`implements` clause, and member signatures. tokens[i] is
// "class".
func handleClassDecl(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	*out = append(*out, tokens[i])
	i++
	if j := nextSignificant(tokens, i); j >= 0 && tokens[j].kind == "ident" {
		for k := i; k <= j; k++ {
			*out = append(*out, tokens[k])
		}
		i = j + 1
	}
	i = stripOptionalTypeParams(tokens, i, out)
	i = handleExtendsImplements(tokens, i, out)

	open := nextSignificant(tokens, i)
	if open < 0 || tokens[open].kind != "punct" || tokens[open].text != "{" {
		return i, nil
	}
	for k := i; k < open; k++ {
		*out = append(*out, tokens[k])
	}
	*out = append(*out, tokens[open])
	end, err := stripClassBody(tokens, open+1, out)
	if err != nil {
		return end, err
	}
	if end < len(tokens) {
		*out = append(*out, tokens[end])
		end++
	}
	return end, nil
}

// handleExtendsImplements copies a class's `extends Base<T>` clause
// (keeping the real base-class reference, only stripping its type
// arguments) and drops an `implements A<T>, B` clause entirely (TypeScript
// syntax with no runtime existence). It stops right at the class body's
// "{", without consuming it.
func handleExtendsImplements(tokens []tsToken, i int, out *[]tsToken) int {
	for {
		j := nextSignificant(tokens, i)
		if j < 0 {
			return i
		}
		if tokens[j].kind == "punct" && tokens[j].text == "{" {
			return i
		}
		if tokens[j].kind == "ident" && tokens[j].text == "extends" {
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = copyHeritageRef(tokens, j+1, out)
			continue
		}
		if tokens[j].kind == "ident" && tokens[j].text == "implements" {
			end := scanUntilTopLevelBrace(tokens, j)
			for end > j && (tokens[end-1].kind == "space" || tokens[end-1].kind == "comment") {
				end--
			}
			return end
		}
		return i
	}
}

// copyHeritageRef copies an `extends` target expression (a dotted
// identifier chain, optionally called, e.g. a mixin pattern like
// `mixin(Base)`) verbatim except for stripping a trailing `<...>`
// type-argument list on any identifier in it, stopping at the class body's
// `{` or an `implements` clause.
func copyHeritageRef(tokens []tsToken, i int, out *[]tsToken) int {
	for {
		j := nextSignificant(tokens, i)
		if j < 0 {
			return i
		}
		if tokens[j].kind == "punct" && tokens[j].text == "{" {
			return i
		}
		if tokens[j].kind == "ident" && tokens[j].text == "implements" {
			return i
		}
		switch {
		case tokens[j].kind == "ident":
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
			if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "<" {
				if end, ok := consumeAngleTypeArgsBalanced(tokens, p); ok {
					for k := i; k < p; k++ {
						*out = append(*out, tokens[k])
					}
					i = end
				}
			}
		case tokens[j].kind == "punct" && tokens[j].text == ".":
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
		case tokens[j].kind == "punct" && tokens[j].text == "(":
			for k := i; k < j; k++ {
				*out = append(*out, tokens[k])
			}
			*out = append(*out, tokens[j])
			end, err := stripBody(tokens, j+1, out, stopAtPunct(")"))
			if err != nil {
				return end
			}
			if end < len(tokens) {
				*out = append(*out, tokens[end])
				end++
			}
			i = end
		default:
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
		}
	}
}

var classMemberTSModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "readonly": true,
	"override": true, "declare": true,
}

var classMemberJSModifiers = map[string]bool{
	"static": true, "async": true, "get": true, "set": true, "accessor": true,
}

// stripClassBody strips every member of a class body starting at
// tokens[i] (right after the opening "{"), stopping at (and not
// consuming) the matching "}".
func stripClassBody(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	for {
		j := nextSignificant(tokens, i)
		if j < 0 {
			for k := i; k < len(tokens); k++ {
				*out = append(*out, tokens[k])
			}
			return len(tokens), nil
		}
		if tokens[j].kind == "punct" && tokens[j].text == "}" {
			for k := i; k < j; k++ {
				*out = append(*out, tokens[k])
			}
			return j, nil
		}
		if tokens[j].kind == "punct" && tokens[j].text == ";" {
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			i = j + 1
			continue
		}
		next, err := stripClassMember(tokens, i, out)
		if err != nil {
			return next, err
		}
		if next == i {
			// Defensive: guarantee forward progress on unrecognized input.
			for k := i; k <= j; k++ {
				*out = append(*out, tokens[k])
			}
			next = j + 1
		}
		i = next
	}
}

// stripClassMember strips one class member: a static initialization
// block, or a (possibly modified, possibly generic, possibly abstract)
// field or method. A bodyless method signature (an overload declaration or
// an `abstract` method) is erased entirely, matching
// stripTypeScriptTypes's erasure of a function overload signature (see
// handleFunctionSignature and strip_types_test.go).
func stripClassMember(tokens []tsToken, i int, out *[]tsToken) (int, error) {
	memberStart := len(*out)
	j := nextSignificant(tokens, i)
	if j < 0 {
		return i, nil
	}
	for k := i; k < j; k++ {
		*out = append(*out, tokens[k])
	}
	i = j

	if tokens[i].kind == "ident" && tokens[i].text == "static" {
		n := nextSignificant(tokens, i+1)
		if n >= 0 && tokens[n].kind == "punct" && tokens[n].text == "{" {
			for k := i; k <= n; k++ {
				*out = append(*out, tokens[k])
			}
			end, err := stripBody(tokens, n+1, out, stopAtPunct("}"))
			if err != nil {
				return end, err
			}
			if end < len(tokens) {
				*out = append(*out, tokens[end])
				end++
			}
			return end, nil
		}
	}

	for {
		k := nextSignificant(tokens, i)
		if k < 0 || tokens[k].kind != "ident" {
			break
		}
		// A modifier keyword directly followed by "(" is actually the
		// member's own name (e.g. a method literally named `get` or
		// `static`), not a modifier -- stop here and let copyMemberName
		// treat it as the name.
		if after := nextSignificant(tokens, k+1); after >= 0 && tokens[after].kind == "punct" && tokens[after].text == "(" {
			break
		}
		if classMemberTSModifiers[tokens[k].text] {
			i = k + 1
			continue
		}
		if tokens[k].text == "abstract" {
			i = k + 1
			continue
		}
		if classMemberJSModifiers[tokens[k].text] {
			for m := i; m <= k; m++ {
				*out = append(*out, tokens[m])
			}
			i = k + 1
			continue
		}
		break
	}

	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "*" {
		for k := i; k <= p; k++ {
			*out = append(*out, tokens[k])
		}
		i = p + 1
	}

	nameEnd, ok, isCtor := copyMemberName(tokens, i, out)
	if !ok {
		n := nextSignificant(tokens, i)
		if n < 0 {
			return len(tokens), nil
		}
		for k := i; k <= n; k++ {
			*out = append(*out, tokens[k])
		}
		return n + 1, nil
	}
	i = nameEnd

	i = stripOptionalMark(tokens, i, out, "?")
	i = stripOptionalMark(tokens, i, out, "!")

	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "<" {
		if end, ok2 := consumeAngleTypeArgsBalanced(tokens, p); ok2 {
			for k := i; k < p; k++ {
				*out = append(*out, tokens[k])
			}
			i = end
		}
	}

	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "(" {
		for k := i; k < p; k++ {
			*out = append(*out, tokens[k])
		}
		var err error
		i, err = stripParamList(tokens, p, out, isCtor)
		if err != nil {
			return i, err
		}
		i = stripOptionalReturnType(tokens, i, out)
		n := nextSignificant(tokens, i)
		if n >= 0 && tokens[n].kind == "punct" && tokens[n].text == "{" {
			for k := i; k < n; k++ {
				*out = append(*out, tokens[k])
			}
			*out = append(*out, tokens[n])
			end, err2 := stripBody(tokens, n+1, out, stopAtPunct("}"))
			if err2 != nil {
				return end, err2
			}
			if end < len(tokens) {
				*out = append(*out, tokens[end])
				end++
			}
			return end, nil
		}
		end := scanToStatementEnd(tokens, i)
		*out = (*out)[:memberStart]
		return end, nil
	}

	// Field.
	i = stripOptionalTypeAnnotation(tokens, i, out, false)
	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "=" {
		for k := i; k <= p; k++ {
			*out = append(*out, tokens[k])
		}
		i = p + 1
		var err error
		i, err = stripBody(tokens, i, out, stopAtAny(";"))
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

// copyMemberName copies a class member's name -- a plain identifier, a
// string/number literal name, a `#private` field name, or a `[computed]`
// name (copied verbatim, bracket-matched, without recursing into it: a
// computed class member name containing further TypeScript syntax is rare
// enough not to be worth the added complexity here) -- returning the index
// just past it, whether a name was recognized, and whether it is exactly
// `constructor` (which gates constructor-parameter-property detection in
// stripParamList).
func copyMemberName(tokens []tsToken, i int, out *[]tsToken) (int, bool, bool) {
	j := nextSignificant(tokens, i)
	if j < 0 {
		return i, false, false
	}
	for k := i; k < j; k++ {
		*out = append(*out, tokens[k])
	}
	t := tokens[j]
	switch {
	case t.kind == "punct" && t.text == "#":
		*out = append(*out, t)
		n := j + 1
		if n < len(tokens) && tokens[n].kind == "ident" {
			*out = append(*out, tokens[n])
			return n + 1, true, false
		}
		return n, false, false
	case t.kind == "punct" && t.text == "[":
		close := matchParen(tokens, j)
		if close < 0 {
			return j, false, false
		}
		for k := j; k <= close; k++ {
			*out = append(*out, tokens[k])
		}
		return close + 1, true, false
	case t.kind == "ident" || t.kind == "string" || t.kind == "number":
		*out = append(*out, t)
		return j + 1, true, t.kind == "ident" && t.text == "constructor"
	default:
		return j, false, false
	}
}
