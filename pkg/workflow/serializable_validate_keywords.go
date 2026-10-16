package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// appendContainsError mirrors Ajv's contains keyword. minContains and
// maxContains only apply under draft 2020-12 (Ajv2020 sets opts.next).
// Items are checked in order and the scan stops as soon as the outcome is
// known; when the keyword holds, the errors of the items that did not match
// are dropped, when it fails they are reported before the message.
func (w *ajvWalker) appendContainsError(arr []interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	contains, has := sch["contains"]
	if !has {
		return out
	}
	min, max, hasMax := 1.0, 0.0, false
	if w.draft2020 {
		if m, ok := schemaBound(sch, "minContains"); ok {
			min = m
		}
		if m, ok := schemaBound(sch, "maxContains"); ok {
			max, hasMax = m, true
		}
	}
	if !hasMax && min == 0 {
		return out
	}
	msg := fmt.Sprintf("%s must contain at least %s valid item(s)", path, formatJSNumber(min))
	if hasMax {
		msg = fmt.Sprintf("%s must contain at least %s and no more than %s valid item(s)", path, formatJSNumber(min), formatJSNumber(max))
		if min > max {
			return append(out, msg)
		}
	}
	var collected []string
	count := 0.0
	for i := range arr {
		errs := w.errorsAny(arr[i], contains, hintChild(hint, "contains"), fmt.Sprintf("%s/%d", path, i), nil, depth+1)
		if len(errs) > 0 {
			collected = append(collected, errs...)
			continue
		}
		count++
		if hasMax {
			if count > max {
				break
			}
		} else if count >= min {
			break
		}
	}
	if count >= min && (!hasMax || count <= max) {
		return out
	}
	out = append(out, collected...)
	return append(out, msg)
}

// errorsAny validates value against a schema that may be a boolean: true
// accepts everything, false reports "boolean schema is false".
func (w *ajvWalker) errorsAny(value interface{}, raw interface{}, hint interface{}, path string, out []string, depth int) []string {
	switch s := raw.(type) {
	case bool:
		if !s {
			out = append(out, path+" boolean schema is false")
		}
		return out
	case map[string]interface{}:
		return w.errors(value, s, hint, path, out, depth)
	}
	return out
}

func (w *ajvWalker) appendItemsErrors(arr []interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	elem := func(i int, s interface{}, h interface{}) {
		out = w.errorsAny(arr[i], s, h, fmt.Sprintf("%s/%d", path, i), out, depth+1)
	}
	if w.draft2020 {
		prefix := 0
		if pre, ok := sch["prefixItems"].([]interface{}); ok {
			for i, p := range pre {
				if i < len(arr) {
					elem(i, p, hintIndex(hintChild(hint, "prefixItems"), i))
				}
			}
			prefix = len(pre)
		}
		switch items := sch["items"].(type) {
		case bool:
			if !items && len(arr) > prefix {
				out = append(out, fmt.Sprintf("%s must NOT have more than %d items", path, prefix))
			}
		case map[string]interface{}:
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
	case bool:
		if !items {
			for i := range arr {
				elem(i, items, nil)
			}
		}
	case map[string]interface{}:
		for i := range arr {
			elem(i, items, hintChild(hint, "items"))
		}
	case []interface{}:
		for i, it := range items {
			if i < len(arr) {
				elem(i, it, hintIndex(hintChild(hint, "items"), i))
			}
		}
	}
	return out
}

// appendUnevaluatedItemsErrors mirrors Ajv's unevaluatedItems (2020-12): the
// items that no prefixItems, items, contains or applicator subschema covered.
func (w *ajvWalker) appendUnevaluatedItemsErrors(arr []interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	un, has := sch["unevaluatedItems"]
	if !has {
		return out
	}
	evaluated, all := w.evaluatedItems(arr, sch, depth, false)
	if all {
		return out
	}
	switch u := un.(type) {
	case bool:
		if !u && evaluated < len(arr) {
			out = append(out, fmt.Sprintf("%s must NOT have more than %d items", path, evaluated))
		}
	case map[string]interface{}:
		for i := evaluated; i < len(arr); i++ {
			out = w.errors(arr[i], u, hintChild(hint, "unevaluatedItems"), fmt.Sprintf("%s/%d", path, i), out, depth+1)
		}
	}
	return out
}

// evaluatedItems reports how many leading items the schema evaluated, or that
// it evaluated all of them. top skips the schema's own unevaluatedItems.
func (w *ajvWalker) evaluatedItems(arr []interface{}, sch map[string]interface{}, depth int, nested bool) (int, bool) {
	if depth > maxRefDepth {
		return 0, false
	}
	n := 0
	if pre, ok := sch["prefixItems"].([]interface{}); ok && len(pre) > n {
		n = len(pre)
	}
	if tuple, ok := sch["items"].([]interface{}); ok && len(tuple) > n {
		n = len(tuple)
	}
	switch sch["items"].(type) {
	case bool, map[string]interface{}:
		return len(arr), true
	}
	if _, ok := sch["unevaluatedItems"]; ok && nested {
		return len(arr), true
	}
	if c, ok := sch["contains"]; ok && len(arr) > 0 {
		matched := true
		for i := range arr {
			if len(w.errorsAny(arr[i], c, nil, "", nil, depth+1)) != 0 {
				matched = false
				break
			}
		}
		if matched {
			return len(arr), true
		}
	}
	merge := func(sub map[string]interface{}) bool {
		m, all := w.evaluatedItems(arr, sub, depth+1, true)
		if all {
			return true
		}
		if m > n {
			n = m
		}
		return false
	}
	if ref, ok := sch["$ref"].(string); ok {
		if resolved, _ := w.resolveRef(ref); resolved != nil && merge(resolved) {
			return len(arr), true
		}
	}
	if allOf, ok := sch["allOf"].([]interface{}); ok {
		for _, b := range allOf {
			if bm, ok := b.(map[string]interface{}); ok && merge(bm) {
				return len(arr), true
			}
		}
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if branches, ok := sch[key].([]interface{}); ok {
			for _, b := range branches {
				if bm, ok := b.(map[string]interface{}); ok && len(w.errors(arr, bm, nil, "", nil, depth+1)) == 0 && merge(bm) {
					return len(arr), true
				}
			}
		}
	}
	if cond, ok := sch["if"].(map[string]interface{}); ok {
		branch := "else"
		if len(w.errors(arr, cond, nil, "", nil, depth+1)) == 0 {
			branch = "then"
			if merge(cond) {
				return len(arr), true
			}
		}
		if bm, ok := sch[branch].(map[string]interface{}); ok && merge(bm) {
			return len(arr), true
		}
	}
	return n, false
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
	if names, ok := sch["propertyNames"]; ok {
		for _, k := range sortedObjectKeys(obj) {
			if errs := w.errorsAny(k, names, hintChild(hint, "propertyNames"), path, nil, depth+1); len(errs) > 0 {
				out = append(out, errs...)
				out = append(out, path+" property name must be valid")
			}
		}
	}
	props, _ := sch["properties"].(map[string]interface{})
	patternSchemas, _ := sch["patternProperties"].(map[string]interface{})
	patternKeys := orderedKeys(patternSchemas, hintChild(hint, "patternProperties"))
	patterns := make([]*regexp.Regexp, len(patternKeys))
	for i, p := range patternKeys {
		if re, err := regexp.Compile(p); err == nil {
			patterns[i] = re
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
	out = w.appendDependencyErrors(obj, sch["dependencies"], hintChild(hint, "dependencies"), "dependencies", path, out, depth)
	propsHint := hintChild(hint, "properties")
	for _, k := range orderedKeys(props, propsHint) {
		if child, exists := obj[k]; exists {
			out = w.errorsAny(child, props[k], hintChild(propsHint, k), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	for i, p := range patternKeys {
		if patterns[i] == nil {
			continue
		}
		var keys []string
		for k := range obj {
			if patterns[i].MatchString(k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = w.errorsAny(obj[k], patternSchemas[p], hintChild(hintChild(hint, "patternProperties"), p), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	if w.draft2020 {
		out = w.appendDependencyErrors(obj, sch["dependentRequired"], nil, "dependentRequired", path, out, depth)
		out = w.appendDependencyErrors(obj, sch["dependentSchemas"], hintChild(hint, "dependentSchemas"), "dependentSchemas", path, out, depth)
		out = w.appendUnevaluatedPropertiesErrors(obj, sch, hint, path, out, depth)
	}
	return out
}

func sortedObjectKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// appendDependencyErrors mirrors Ajv's dependencies / dependentRequired /
// dependentSchemas: property dependencies (arrays) first, one message per
// missing property, then schema dependencies.
func (w *ajvWalker) appendDependencyErrors(obj map[string]interface{}, raw interface{}, hint interface{}, keyword string, path string, out []string, depth int) []string {
	deps, _ := raw.(map[string]interface{})
	if len(deps) == 0 {
		return out
	}
	keys := orderedKeys(deps, hint)
	for _, prop := range keys {
		list, ok := deps[prop].([]interface{})
		if !ok || len(list) == 0 {
			continue
		}
		if _, present := obj[prop]; !present {
			continue
		}
		names := make([]string, 0, len(list))
		for _, d := range list {
			names = append(names, fmt.Sprint(d))
		}
		noun := "properties"
		if len(list) == 1 {
			noun = "property"
		}
		msg := fmt.Sprintf("%s must have %s %s when property %s is present", path, noun, strings.Join(names, ", "), prop)
		for _, d := range list {
			if name, ok := d.(string); ok {
				if _, exists := obj[name]; !exists {
					out = append(out, msg)
				}
			}
		}
	}
	for _, prop := range keys {
		if _, isList := deps[prop].([]interface{}); isList {
			continue
		}
		if _, present := obj[prop]; !present {
			continue
		}
		out = w.errorsAny(obj, deps[prop], hintChild(hint, prop), path, out, depth+1)
	}
	return out
}

// appendUnevaluatedPropertiesErrors mirrors Ajv's unevaluatedProperties
// (2020-12): one error per property that no properties, patternProperties,
// additionalProperties or applicator subschema evaluated.
func (w *ajvWalker) appendUnevaluatedPropertiesErrors(obj map[string]interface{}, sch map[string]interface{}, hint interface{}, path string, out []string, depth int) []string {
	un, has := sch["unevaluatedProperties"]
	if !has {
		return out
	}
	evaluated, all := w.evaluatedProps(obj, sch, depth, false)
	if all {
		return out
	}
	for _, k := range sortedObjectKeys(obj) {
		if evaluated[k] {
			continue
		}
		switch u := un.(type) {
		case bool:
			if !u {
				out = append(out, path+" must NOT have unevaluated properties")
			}
		case map[string]interface{}:
			out = w.errors(obj[k], u, hintChild(hint, "unevaluatedProperties"), path+"/"+escapeInstancePathKey(k), out, depth+1)
		}
	}
	return out
}

// evaluatedProps returns the property names the schema evaluated, or that it
// evaluated all of them. nested ignores the schema's own unevaluatedProperties
// at the top level only.
func (w *ajvWalker) evaluatedProps(obj map[string]interface{}, sch map[string]interface{}, depth int, nested bool) (map[string]bool, bool) {
	evaluated := map[string]bool{}
	if depth > maxRefDepth {
		return evaluated, false
	}
	if props, ok := sch["properties"].(map[string]interface{}); ok {
		for k := range props {
			if _, present := obj[k]; present {
				evaluated[k] = true
			}
		}
	}
	if pp, ok := sch["patternProperties"].(map[string]interface{}); ok {
		for p := range pp {
			if re, err := regexp.Compile(p); err == nil {
				for k := range obj {
					if re.MatchString(k) {
						evaluated[k] = true
					}
				}
			}
		}
	}
	switch sch["additionalProperties"].(type) {
	case bool, map[string]interface{}:
		return evaluated, true
	}
	if _, ok := sch["unevaluatedProperties"]; ok && nested {
		return evaluated, true
	}
	merge := func(sub map[string]interface{}) bool {
		m, all := w.evaluatedProps(obj, sub, depth+1, true)
		if all {
			return true
		}
		for k := range m {
			evaluated[k] = true
		}
		return false
	}
	if ref, ok := sch["$ref"].(string); ok {
		if resolved, _ := w.resolveRef(ref); resolved != nil && merge(resolved) {
			return evaluated, true
		}
	}
	if allOf, ok := sch["allOf"].([]interface{}); ok {
		for _, b := range allOf {
			if bm, ok := b.(map[string]interface{}); ok && merge(bm) {
				return evaluated, true
			}
		}
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if branches, ok := sch[key].([]interface{}); ok {
			for _, b := range branches {
				if bm, ok := b.(map[string]interface{}); ok && len(w.errors(obj, bm, nil, "", nil, depth+1)) == 0 && merge(bm) {
					return evaluated, true
				}
			}
		}
	}
	if cond, ok := sch["if"].(map[string]interface{}); ok {
		branch := "else"
		if len(w.errors(obj, cond, nil, "", nil, depth+1)) == 0 {
			branch = "then"
			if merge(cond) {
				return evaluated, true
			}
		}
		if bm, ok := sch[branch].(map[string]interface{}); ok && merge(bm) {
			return evaluated, true
		}
	}
	if ds, ok := sch["dependentSchemas"].(map[string]interface{}); ok {
		for prop, sub := range ds {
			if _, present := obj[prop]; !present {
				continue
			}
			if bm, ok := sub.(map[string]interface{}); ok && merge(bm) {
				return evaluated, true
			}
		}
	}
	return evaluated, false
}
