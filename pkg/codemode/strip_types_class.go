package codemode

import "strings"

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
	heritageStart := i
	i = handleExtendsImplements(tokens, i, out)

	open := nextSignificant(tokens, i)
	if open < 0 || tokens[open].kind != "punct" || tokens[open].text != "{" {
		return i, nil
	}
	hasExtends := false
	for k := heritageStart; k < open; k++ {
		if tokens[k].kind == "ident" && tokens[k].text == "extends" {
			hasExtends = true
			break
		}
	}
	for k := i; k < open; k++ {
		*out = append(*out, tokens[k])
	}
	*out = append(*out, tokens[open])
	end, err := stripClassBody(tokens, open+1, out, hasExtends)
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
func stripClassBody(tokens []tsToken, i int, out *[]tsToken, hasExtends bool) (int, error) {
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
		next, err := stripClassMember(tokens, i, out, hasExtends)
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
func stripClassMember(tokens []tsToken, i int, out *[]tsToken, hasExtends bool) (int, error) {
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

	sawAbstract := false
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
		if tokens[k].text == "declare" {
			// A `declare` field is ambient: Node (24.20) erases the whole
			// member, like an abstract one.
			sawAbstract = true
			i = k + 1
			continue
		}
		if tokens[k].text == "override" && !hasExtends {
			// Node rejects an `override` member in a class with no base class.
			return k, errUnsupportedTSSyntax("override without extends")
		}
		if classMemberTSModifiers[tokens[k].text] {
			i = k + 1
			continue
		}
		if tokens[k].text == "abstract" {
			sawAbstract = true
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

	// An index signature (`[key: string]: any;`) has no runtime form and is
	// erased whole.
	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "[" {
		if q := nextSignificant(tokens, p+1); q >= 0 && tokens[q].kind == "ident" {
			if c := nextSignificant(tokens, q+1); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == ":" {
				if closeIdx := matchParen(tokens, p); closeIdx >= 0 {
					end := closeIdx + 1
					if m := nextSignificant(tokens, end); m >= 0 && tokens[m].kind == "punct" && tokens[m].text == ":" {
						end = consumeTypeExpr(tokens, m+1, false)
					}
					if semi := nextSignificant(tokens, end); semi >= 0 && tokens[semi].kind == "punct" && tokens[semi].text == ";" {
						end = semi + 1
					}
					*out = (*out)[:memberStart]
					return end, nil
				}
			}
		}
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
		// A bodyless signature (an overload or abstract method) ends with
		// its return type, or with a `;` after it.
		end := i
		end = consumeOptionalSemicolon(tokens, end)
		*out = (*out)[:memberStart]
		return end, nil
	}

	// Field.
	if sawAbstract {
		// An abstract field has no runtime form: the whole member goes.
		end := i
		if c := nextSignificant(tokens, i); c >= 0 && tokens[c].kind == "punct" && tokens[c].text == ":" {
			end = consumeTypeExpr(tokens, c+1, false)
		}
		end = consumeOptionalSemicolon(tokens, end)
		*out = (*out)[:memberStart]
		return end, nil
	}
	i = stripOptionalTypeAnnotation(tokens, i, out, false)
	if p := nextSignificant(tokens, i); p >= 0 && tokens[p].kind == "punct" && tokens[p].text == "=" {
		for k := i; k <= p; k++ {
			*out = append(*out, tokens[k])
		}
		i = p + 1
		end := fieldInitializerEnd(tokens, i)
		var err error
		i, err = stripBody(tokens[:end], i, out, stopAtAny(";"))
		if err != nil {
			return i, err
		}
		if i >= end {
			i = end
		}
	}
	return i, nil
}

// fieldInitializerEnd returns where a class field's initializer expression
// ends: at the `;` or the class body's `}`, or, when the field has no
// semicolon, where automatic semicolon insertion ends it: a line break after a
// complete operand followed by a token that starts a new member.
func fieldInitializerEnd(tokens []tsToken, i int) int {
	depth := 0
	sawNewline := false
	var prev *tsToken
	for ; i < len(tokens); i++ {
		t := tokens[i]
		if t.kind == "space" || t.kind == "comment" {
			if strings.ContainsAny(t.text, "\n\r") {
				sawNewline = true
			}
			continue
		}
		if depth == 0 {
			if t.kind == "punct" && (t.text == ";" || t.text == "}") {
				return i
			}
			if sawNewline && prev != nil && expressionOperandEnds(prev) && startsNewMember(t) {
				return i
			}
		}
		sawNewline = false
		if t.kind == "punct" {
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
		}
		prev = &tokens[i]
	}
	return len(tokens)
}

func expressionOperandEnds(t *tsToken) bool {
	switch t.kind {
	case "ident":
		return !isExpressionPrefixKeyword(t.text)
	case "number", "string", "regex":
		return true
	case "punct":
		switch t.text {
		case ")", "]", "}":
			return true
		}
	}
	return false
}

// startsNewMember reports whether t, on a new line after a complete
// expression, cannot continue it and so begins the next class member.
func startsNewMember(t tsToken) bool {
	switch t.kind {
	case "ident":
		switch t.text {
		case "in", "instanceof", "of", "as", "satisfies":
			return false
		}
		return true
	case "string", "number":
		return true
	case "punct":
		return t.text == "#" || t.text == "@"
	}
	return false
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
