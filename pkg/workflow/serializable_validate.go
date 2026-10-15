package workflow

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// collectActionableErrors reports the schema violations of value the way TS
// does for tools without their own validator: Ajv with allErrors, each error
// run through formatAjvError and joined by ", " (#22392). Errors come out in
// Ajv's evaluation order: the type check (up front, or after the type-less
// keywords when the schema has rules for that single type), then const, enum,
// not, anyOf, oneOf, allOf, if/then/else, then the number, string, array and
// object keyword groups; properties follow the schema's key order (hint is the
// ordered tree of the schema, or nil for sorted order). Keywords this walker
// does not know are left to the generic validator text.
func collectActionableErrors(value interface{}, sch map[string]interface{}, hint interface{}, path string) []string {
	w := &ajvWalker{root: sch, rootHint: hint}
	if d, ok := sch["$schema"].(string); ok && strings.TrimSuffix(d, "#") == "https://json-schema.org/draft/2020-12/schema" {
		w.draft2020 = true
	}
	return w.errors(value, sch, hint, path, nil, 0)
}

type ajvWalker struct {
	root      map[string]interface{}
	rootHint  interface{}
	draft2020 bool
}

const maxRefDepth = 64

func hintChild(hint interface{}, key string) interface{} {
	o, _ := hint.(*jsonorder.Object)
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	return v
}

func hintIndex(hint interface{}, i int) interface{} {
	arr, _ := hint.([]interface{})
	if i < 0 || i >= len(arr) {
		return nil
	}
	return arr[i]
}

// orderedKeys returns the keys of m: those the hint lists first in hint order,
// then the rest sorted.
func orderedKeys(m map[string]interface{}, hint interface{}) []string {
	keys := make([]string, 0, len(m))
	seen := make(map[string]bool, len(m))
	if o, _ := hint.(*jsonorder.Object); o != nil {
		for _, k := range o.Keys() {
			if _, ok := m[k]; ok && !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func (w *ajvWalker) resolveRef(ref string) (map[string]interface{}, interface{}) {
	if !strings.HasPrefix(ref, "#") {
		return nil, nil
	}
	var cur interface{} = w.root
	hint := w.rootHint
	frag := strings.TrimPrefix(ref, "#")
	if frag == "" {
		return w.root, w.rootHint
	}
	if !strings.HasPrefix(frag, "/") {
		return nil, nil
	}
	for _, part := range strings.Split(frag[1:], "/") {
		if dec, err := url.PathUnescape(part); err == nil {
			part = dec
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch c := cur.(type) {
		case map[string]interface{}:
			cur = c[part]
			hint = hintChild(hint, part)
		case []interface{}:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return nil, nil
			}
			cur = c[i]
			hint = hintIndex(hint, i)
		default:
			return nil, nil
		}
	}
	m, _ := cur.(map[string]interface{})
	return m, hint
}

func hasAny(sch map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if _, ok := sch[k]; ok {
			return true
		}
	}
	return false
}

var (
	numberKeys = []string{"maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum", "multipleOf"}
	stringKeys = []string{"maxLength", "minLength", "pattern"}
	arrayKeys  = []string{"maxItems", "minItems", "uniqueItems", "items", "additionalItems", "prefixItems", "contains"}
	objectKeys = []string{"maxProperties", "minProperties", "required", "propertyNames", "additionalProperties", "dependencies", "properties", "patternProperties"}
)

func groupKeys(typeName string) []string {
	switch typeName {
	case "number":
		return numberKeys
	case "string":
		return stringKeys
	case "array":
		return arrayKeys
	case "object":
		return objectKeys
	}
	return nil
}

func schemaTypeNames(sch map[string]interface{}) []string {
	switch t := sch["type"].(type) {
	case string:
		return []string{t}
	case []interface{}:
		var names []string
		for _, e := range t {
			if n, ok := e.(string); ok {
				names = append(names, n)
			}
		}
		return names
	case []string:
		return t
	}
	return nil
}

func (w *ajvWalker) errors(value interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	if sch == nil || depth > maxRefDepth {
		return out
	}
	// Ajv checks a single type inside the keyword group of that type when the
	// schema has rules for it, and up front otherwise.
	names := schemaTypeNames(sch)
	deferred := false
	if len(names) == 1 {
		if keys := groupKeys(names[0]); keys != nil && hasAny(sch, keys...) {
			deferred = true
		}
	}
	typeMessage := ""
	typeOK := true
	if len(names) > 0 {
		typeOK = false
		for _, n := range names {
			if matchesJSONType(value, n) {
				typeOK = true
				break
			}
		}
		typeMessage = fmt.Sprintf("%s must be %s", path, strings.Join(names, ","))
	}
	if !typeOK && !deferred {
		out = append(out, typeMessage)
	}

	// Keywords without a type: $ref (core vocabulary), const, enum, not,
	// anyOf, oneOf, allOf, if. Ajv 8 evaluates $ref next to its siblings.
	if ref, ok := sch["$ref"].(string); ok {
		if resolved, rhint := w.resolveRef(ref); resolved != nil {
			out = w.errors(value, resolved, rhint, path, out, depth+1)
		}
	}
	if c, ok := sch["const"]; ok && !jsonEqual(value, c) {
		out = append(out, fmt.Sprintf("%s must equal %s", path, jsonString(c)))
	}
	if enum, ok := sch["enum"].([]interface{}); ok && len(enum) > 0 {
		matched := false
		for _, e := range enum {
			if jsonEqual(value, e) {
				matched = true
				break
			}
		}
		if !matched {
			parts := make([]string, len(enum))
			for i, e := range enum {
				parts[i] = jsonString(e)
			}
			out = append(out, fmt.Sprintf("%s must be one of %s", path, strings.Join(parts, ", ")))
		}
	}
	if not, ok := sch["not"].(map[string]interface{}); ok {
		if len(w.errors(value, not, hintChild(hint, "not"), path, nil, depth+1)) == 0 {
			out = append(out, path+" must NOT be valid")
		}
	}
	if anyOf, ok := sch["anyOf"].([]interface{}); ok {
		var branchErrs []string
		valid := false
		for i, b := range anyOf {
			bm, _ := b.(map[string]interface{})
			errs := w.errors(value, bm, hintIndex(hintChild(hint, "anyOf"), i), path, nil, depth+1)
			if len(errs) == 0 {
				valid = true
				break
			}
			branchErrs = append(branchErrs, errs...)
		}
		if !valid {
			out = append(out, branchErrs...)
			out = append(out, path+" must match a schema in anyOf")
		}
	}
	if oneOf, ok := sch["oneOf"].([]interface{}); ok {
		var branchErrs []string
		passing := 0
		for i, b := range oneOf {
			bm, _ := b.(map[string]interface{})
			errs := w.errors(value, bm, hintIndex(hintChild(hint, "oneOf"), i), path, nil, depth+1)
			if len(errs) == 0 {
				passing++
			} else {
				branchErrs = append(branchErrs, errs...)
			}
		}
		if passing != 1 {
			out = append(out, branchErrs...)
			out = append(out, path+" must match exactly one schema in oneOf")
		}
	}
	if allOf, ok := sch["allOf"].([]interface{}); ok {
		for i, b := range allOf {
			if bm, ok := b.(map[string]interface{}); ok {
				out = w.errors(value, bm, hintIndex(hintChild(hint, "allOf"), i), path, out, depth+1)
			}
		}
	}
	if cond, ok := sch["if"].(map[string]interface{}); ok {
		branch := "else"
		if len(w.errors(value, cond, hintChild(hint, "if"), path, nil, depth+1)) == 0 {
			branch = "then"
		}
		if bm, ok := sch[branch].(map[string]interface{}); ok {
			if errs := w.errors(value, bm, hintChild(hint, branch), path, nil, depth+1); len(errs) > 0 {
				out = append(out, errs...)
				out = append(out, fmt.Sprintf("%s must match %q schema", path, branch))
			}
		}
	}

	// Typed keyword groups, in Ajv's order.
	if f, ok := jsonNumber(value); ok {
		out = appendNumberErrors(f, sch, path, out)
	} else if len(names) == 1 && names[0] == "number" && !typeOK && deferred {
		out = append(out, typeMessage)
	}
	if str, ok := value.(string); ok {
		out = appendStringErrors(str, sch, path, out)
	} else if !typeOK && deferred && names[0] == "string" {
		out = append(out, typeMessage)
	}
	if arr, ok := value.([]interface{}); ok {
		out = w.appendArrayErrors(arr, sch, hint, path, out, depth)
	} else if !typeOK && deferred && names[0] == "array" {
		out = append(out, typeMessage)
	}
	if obj, ok := value.(map[string]interface{}); ok {
		out = w.appendObjectErrors(obj, sch, hint, path, out, depth)
	} else if !typeOK && deferred && names[0] == "object" {
		out = append(out, typeMessage)
	}
	return out
}

func schemaBound(sch map[string]interface{}, key string) (float64, bool) {
	v, ok := sch[key]
	if !ok {
		return 0, false
	}
	return jsonNumber(v)
}

func appendNumberErrors(f float64, sch map[string]interface{}, path string, out []string) []string {
	if m, has := schemaBound(sch, "maximum"); has && f > m {
		out = append(out, fmt.Sprintf("%s must be <= %s", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "minimum"); has && f < m {
		out = append(out, fmt.Sprintf("%s must be >= %s", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "exclusiveMaximum"); has && f >= m {
		out = append(out, fmt.Sprintf("%s must be < %s", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "exclusiveMinimum"); has && f <= m {
		out = append(out, fmt.Sprintf("%s must be > %s", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "multipleOf"); has && m != 0 {
		if q := f / m; q != math.Trunc(q) {
			out = append(out, fmt.Sprintf("%s must be multiple of %s", path, formatJSNumber(m)))
		}
	}
	return out
}

func appendStringErrors(str string, sch map[string]interface{}, path string, out []string) []string {
	n := utf8.RuneCountInString(str)
	if m, has := schemaBound(sch, "maxLength"); has && float64(n) > m {
		out = append(out, fmt.Sprintf("%s must NOT have more than %s characters", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "minLength"); has && float64(n) < m {
		out = append(out, fmt.Sprintf("%s must NOT have fewer than %s characters", path, formatJSNumber(m)))
	}
	if pat, ok := sch["pattern"].(string); ok {
		if re, err := regexp.Compile(pat); err == nil && !re.MatchString(str) {
			out = append(out, fmt.Sprintf("%s must match pattern %q", path, pat))
		}
	}
	return out
}

func (w *ajvWalker) appendArrayErrors(arr []interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	if m, has := schemaBound(sch, "maxItems"); has && float64(len(arr)) > m {
		out = append(out, fmt.Sprintf("%s must NOT have more than %s items", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "minItems"); has && float64(len(arr)) < m {
		out = append(out, fmt.Sprintf("%s must NOT have fewer than %s items", path, formatJSNumber(m)))
	}
	elem := func(i int, s map[string]interface{}, h interface{}) {
		out = w.errors(arr[i], s, h, fmt.Sprintf("%s/%d", path, i), out, depth+1)
	}
	prefix := 0
	if w.draft2020 {
		if pre, ok := sch["prefixItems"].([]interface{}); ok {
			for i, p := range pre {
				if pm, ok := p.(map[string]interface{}); ok && i < len(arr) {
					elem(i, pm, hintIndex(hintChild(hint, "prefixItems"), i))
				}
			}
			prefix = len(pre)
		}
		if items, ok := sch["items"].(map[string]interface{}); ok {
			for i := prefix; i < len(arr); i++ {
				elem(i, items, hintChild(hint, "items"))
			}
		}
		return out
	}
	// Draft-07 evaluates additionalItems before items.
	if tuple, ok := sch["items"].([]interface{}); ok {
		switch add := sch["additionalItems"].(type) {
		case bool:
			if !add && len(arr) > len(tuple) {
				out = append(out, fmt.Sprintf("%s must NOT have more than %d items", path, len(tuple)))
			}
		case map[string]interface{}:
			for i := len(tuple); i < len(arr); i++ {
				elem(i, add, hintChild(hint, "additionalItems"))
			}
		}
	}
	switch items := sch["items"].(type) {
	case map[string]interface{}:
		for i := range arr {
			elem(i, items, hintChild(hint, "items"))
		}
	case []interface{}:
		for i, it := range items {
			if im, ok := it.(map[string]interface{}); ok && i < len(arr) {
				elem(i, im, hintIndex(hintChild(hint, "items"), i))
			}
		}
	}
	return out
}

func (w *ajvWalker) appendObjectErrors(obj map[string]interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	if m, has := schemaBound(sch, "maxProperties"); has && float64(len(obj)) > m {
		out = append(out, fmt.Sprintf("%s must NOT have more than %s properties", path, formatJSNumber(m)))
	}
	if m, has := schemaBound(sch, "minProperties"); has && float64(len(obj)) < m {
		out = append(out, fmt.Sprintf("%s must NOT have fewer than %s properties", path, formatJSNumber(m)))
	}
	if req, ok := sch["required"].([]interface{}); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				if _, exists := obj[name]; !exists {
					out = append(out, fmt.Sprintf("%s is missing required property %s", path, jsonString(name)))
				}
			}
		}
	}
	props, _ := sch["properties"].(map[string]interface{})
	var patterns []*regexp.Regexp
	patternSchemas, _ := sch["patternProperties"].(map[string]interface{})
	patternKeys := orderedKeys(patternSchemas, hintChild(hint, "patternProperties"))
	for _, p := range patternKeys {
		if re, err := regexp.Compile(p); err == nil {
			patterns = append(patterns, re)
		} else {
			patterns = append(patterns, nil)
		}
	}
	extra := make([]string, 0, len(obj))
	for k := range obj {
		if _, declared := props[k]; declared {
			continue
		}
		matched := false
		for _, re := range patterns {
			if re != nil && re.MatchString(k) {
				matched = true
				break
			}
		}
		if !matched {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	switch add := sch["additionalProperties"].(type) {
	case bool:
		if !add {
			for _, k := range extra {
				out = append(out, fmt.Sprintf("%s has unexpected property %s", path, jsonString(k)))
			}
		}
	case map[string]interface{}:
		for _, k := range extra {
			out = w.errors(obj[k], add, hintChild(hint, "additionalProperties"), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	propsHint := hintChild(hint, "properties")
	for _, k := range orderedKeys(props, propsHint) {
		child, exists := obj[k]
		sub, ok := props[k].(map[string]interface{})
		if exists && ok {
			out = w.errors(child, sub, hintChild(propsHint, k), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	for i, p := range patternKeys {
		re := patterns[i]
		sub, ok := patternSchemas[p].(map[string]interface{})
		if re == nil || !ok {
			continue
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			if re.MatchString(k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = w.errors(obj[k], sub, hintChild(hintChild(hint, "patternProperties"), p), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	return out
}
