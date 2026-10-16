package providerutils

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// OptionCheck validates one provider-option value, the way a zod schema member
// does. path names the value in the error text (for example "logitBias").
type OptionCheck func(path string, v interface{}) error

// OptionSchema maps option names to their checks. Like z.object, unknown keys
// pass; every declared key is optional, but an explicit null fails (zod's
// .optional() accepts undefined, not null).
type OptionSchema map[string]OptionCheck

// ParseProviderOptions mirrors TS parseProviderOptions' validation: when
// providerOptions[provider] is absent or null it returns (nil, nil); when it is
// present but not an object or any declared member fails its check it returns an
// InvalidArgumentError ("invalid <provider> provider options") whose Cause
// lists the failing members. The validated object is returned for the caller to
// read.
func ParseProviderOptions(provider string, providerOptions map[string]interface{}, schema OptionSchema) (map[string]interface{}, error) {
	raw, ok := providerOptions[provider]
	if !ok || raw == nil {
		return nil, nil
	}
	obj, isObj := asOptionObject(raw)
	if !isObj {
		return nil, optionsError(provider, fmt.Errorf("invalid input: expected object, received %s", describeOptionValue(raw)))
	}
	keys := make([]string, 0, len(schema))
	for k := range schema {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var problems []string
	for _, k := range keys {
		v, present := obj[k]
		if !present {
			continue
		}
		if err := schema[k](k, v); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return nil, optionsError(provider, fmt.Errorf("%s", strings.Join(problems, "; ")))
	}
	return obj, nil
}

func optionsError(provider string, cause error) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions",
		Message: fmt.Sprintf("invalid %s provider options", provider),
		Cause:   cause,
	}
}

func asOptionObject(v interface{}) (map[string]interface{}, bool) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(map[string]interface{}, rv.Len())
		for _, key := range rv.MapKeys() {
			out[key.String()] = rv.MapIndex(key).Interface()
		}
		return out, true
	}
	return nil, false
}

func describeOptionValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func typeIssue(path, expected string, v interface{}) error {
	return fmt.Errorf("invalid input: expected %s, received %s (at %s)", expected, describeOptionValue(v), path)
}

// OptString is z.string().
func OptString(path string, v interface{}) error {
	if v != nil && reflect.ValueOf(v).Kind() == reflect.String {
		return nil
	}
	return typeIssue(path, "string", v)
}

// OptBool is z.boolean().
func OptBool(path string, v interface{}) error {
	if v != nil && reflect.ValueOf(v).Kind() == reflect.Bool {
		return nil
	}
	return typeIssue(path, "boolean", v)
}

func isOptNumber(v interface{}) bool {
	if v == nil {
		return false
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return false
}

// OptNumber is z.number() (finite).
func OptNumber(path string, v interface{}) error {
	if isOptNumber(v) {
		return nil
	}
	return typeIssue(path, "number", v)
}

// OptBoolOrNumber is z.union([z.boolean(), z.number()]).
func OptBoolOrNumber(path string, v interface{}) error {
	if v != nil && reflect.ValueOf(v).Kind() == reflect.Bool || isOptNumber(v) {
		return nil
	}
	return typeIssue(path, "boolean | number", v)
}

// OptEnum is z.enum([...]).
func OptEnum(values ...string) OptionCheck {
	return func(path string, v interface{}) error {
		if v != nil && reflect.ValueOf(v).Kind() == reflect.String {
			s := reflect.ValueOf(v).String()
			for _, a := range values {
				if s == a {
					return nil
				}
			}
		}
		return fmt.Errorf("invalid option: expected one of %q (at %s)", values, path)
	}
}

// OptRecord is z.record(key, value): a plain object whose keys pass keyCheck
// (nil to accept any) and whose values pass valueCheck.
func OptRecord(keyCheck func(key string) error, valueCheck OptionCheck) OptionCheck {
	return func(path string, v interface{}) error {
		obj, ok := asOptionObject(v)
		if !ok {
			return typeIssue(path, "record", v)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if keyCheck != nil {
				if err := keyCheck(k); err != nil {
					return fmt.Errorf("%s (at %s)", err.Error(), path)
				}
			}
			if valueCheck != nil {
				if err := valueCheck(path+"."+k, obj[k]); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// OptObject is z.object({...}) for a nested object.
func OptObject(schema OptionSchema) OptionCheck {
	return func(path string, v interface{}) error {
		obj, ok := asOptionObject(v)
		if !ok {
			return typeIssue(path, "object", v)
		}
		keys := make([]string, 0, len(schema))
		for k := range schema {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if member, present := obj[k]; present {
				if err := schema[k](path+"."+k, member); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// OptLiteral is z.literal("...").
func OptLiteral(want string) OptionCheck {
	return func(path string, v interface{}) error {
		if v != nil && reflect.ValueOf(v).Kind() == reflect.String && reflect.ValueOf(v).String() == want {
			return nil
		}
		return fmt.Errorf("invalid input: expected %q (at %s)", want, path)
	}
}

// OptStringMax is z.string().max(n) (length in UTF-16 code units like JS).
func OptStringMax(n int) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptString(path, v); err != nil {
			return err
		}
		if len(utf16.Encode([]rune(reflect.ValueOf(v).String()))) > n {
			return fmt.Errorf("too big: expected string to have <=%d characters (at %s)", n, path)
		}
		return nil
	}
}

// OptKeyMax is the key check for z.record(z.string().max(n), ...).
func OptKeyMax(n int) func(string) error {
	return func(key string) error {
		if len(utf16.Encode([]rune(key))) > n {
			return fmt.Errorf("too big: expected string key to have <=%d characters", n)
		}
		return nil
	}
}

// OptKeyNumeric is the key check for z.record(z.coerce.number<string>(), ...):
// the key must coerce (JS Number()) to a finite number.
func OptKeyNumeric(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil // Number("") === 0
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		lower := strings.ToLower(strings.TrimLeft(trimmed, "+-"))
		if !math.IsNaN(f) && !math.IsInf(f, 0) && !strings.HasPrefix(lower, "inf") && !strings.HasPrefix(lower, "nan") {
			return nil
		}
		return fmt.Errorf("invalid input: expected number key, received %q", key)
	}
	if _, err := strconv.ParseInt(trimmed, 0, 64); err == nil {
		return nil
	}
	return fmt.Errorf("invalid input: expected number key, received %q", key)
}

// OptNullable is schema.nullable().optional(): null is accepted.
func OptNullable(check OptionCheck) OptionCheck {
	return func(path string, v interface{}) error {
		if v == nil {
			return nil
		}
		return check(path, v)
	}
}

// OptInt is z.number().int() with optional bounds (nil = unbounded).
func OptInt(min, max *int) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptNumber(path, v); err != nil {
			return err
		}
		f := reflect.ValueOf(v)
		var n float64
		switch f.Kind() {
		case reflect.Float32, reflect.Float64:
			n = f.Float()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n = float64(f.Int())
		default:
			n = float64(f.Uint())
		}
		if n != math.Trunc(n) {
			return typeIssue(path, "int", v)
		}
		if min != nil && n < float64(*min) {
			return fmt.Errorf("too small: expected number to be >=%d (at %s)", *min, path)
		}
		if max != nil && n > float64(*max) {
			return fmt.Errorf("too big: expected number to be <=%d (at %s)", *max, path)
		}
		return nil
	}
}

// OptPositive is z.number().positive() (> 0); integer additionally requires an
// integer value (z.number().int().positive()).
func OptPositive(integer bool) OptionCheck {
	return func(path string, v interface{}) error {
		if integer {
			one := 1
			return OptInt(&one, nil)(path, v)
		}
		if err := OptNumber(path, v); err != nil {
			return err
		}
		f := reflect.ValueOf(v)
		var n float64
		switch f.Kind() {
		case reflect.Float32, reflect.Float64:
			n = f.Float()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n = float64(f.Int())
		default:
			n = float64(f.Uint())
		}
		if n <= 0 {
			return fmt.Errorf("too small: expected number to be >0 (at %s)", path)
		}
		return nil
	}
}

// OptStringLen is z.string().min(min).max(max) (UTF-16 length; max < 0 means no
// upper bound).
func OptStringLen(min, max int) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptString(path, v); err != nil {
			return err
		}
		n := len(utf16.Encode([]rune(reflect.ValueOf(v).String())))
		if n < min {
			return fmt.Errorf("too small: expected string to have >=%d characters (at %s)", min, path)
		}
		if max >= 0 && n > max {
			return fmt.Errorf("too big: expected string to have <=%d characters (at %s)", max, path)
		}
		return nil
	}
}

// OptAnd runs checks in order and returns the first failure.
func OptAnd(checks ...OptionCheck) OptionCheck {
	return func(path string, v interface{}) error {
		for _, c := range checks {
			if err := c(path, v); err != nil {
				return err
			}
		}
		return nil
	}
}

// OptMatch is .regex(re): the value must be a string matching re.
func OptMatch(re *regexp.Regexp, message string) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptString(path, v); err != nil {
			return err
		}
		if !re.MatchString(reflect.ValueOf(v).String()) {
			return fmt.Errorf("%s (at %s)", message, path)
		}
		return nil
	}
}

// OptNumberLiterals is z.union([z.literal(n), ...]) over numbers.
func OptNumberLiterals(values ...float64) OptionCheck {
	return func(path string, v interface{}) error {
		if isOptNumber(v) {
			rv := reflect.ValueOf(v)
			var n float64
			switch rv.Kind() {
			case reflect.Float32, reflect.Float64:
				n = rv.Float()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				n = float64(rv.Int())
			default:
				n = float64(rv.Uint())
			}
			for _, want := range values {
				if n == want {
					return nil
				}
			}
		}
		return fmt.Errorf("invalid input: expected one of %v (at %s)", values, path)
	}
}

// OptTrue is z.literal(true).
func OptTrue(path string, v interface{}) error {
	if b, ok := v.(bool); ok && b {
		return nil
	}
	return fmt.Errorf("invalid input: expected true (at %s)", path)
}

// optFloat returns the numeric value of a number option (checked by OptNumber).
func optFloat(v interface{}) float64 {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	}
	return float64(rv.Uint())
}

// OptRange is z.number().min(min).max(max) (use math.Inf for an open side).
func OptRange(min, max float64) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptNumber(path, v); err != nil {
			return err
		}
		f := optFloat(v)
		if f < min {
			return fmt.Errorf("too small: expected number to be >=%v (at %s)", min, path)
		}
		if f > max {
			return fmt.Errorf("too big: expected number to be <=%v (at %s)", max, path)
		}
		return nil
	}
}

// OptArray is z.array(item) with optional length bounds (max < 0: unbounded).
func OptArray(item OptionCheck, min, max int) OptionCheck {
	return func(path string, v interface{}) error {
		var items []interface{}
		switch list := v.(type) {
		case []interface{}:
			items = list
		default:
			rv := reflect.ValueOf(v)
			if v == nil || rv.Kind() != reflect.Slice {
				return typeIssue(path, "array", v)
			}
			items = make([]interface{}, rv.Len())
			for i := range items {
				items[i] = rv.Index(i).Interface()
			}
		}
		if len(items) < min {
			return fmt.Errorf("too small: expected array to have >=%d items (at %s)", min, path)
		}
		if max >= 0 && len(items) > max {
			return fmt.Errorf("too big: expected array to have <=%d items (at %s)", max, path)
		}
		if item != nil {
			for i, it := range items {
				if err := item(fmt.Sprintf("%s[%d]", path, i), it); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// OptURL is z.url(): a string that parses as an absolute URL. httpsOnly adds
// z.url().startsWith("https://").
func OptURL(httpsOnly bool) OptionCheck {
	return func(path string, v interface{}) error {
		if err := OptString(path, v); err != nil {
			return err
		}
		s := reflect.ValueOf(v).String()
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || (u.Host == "" && u.Opaque == "") {
			return fmt.Errorf("invalid string: expected a valid URL (at %s)", path)
		}
		if httpsOnly && !strings.HasPrefix(s, "https://") {
			return fmt.Errorf("invalid string: expected string to start with \"https://\" (at %s)", path)
		}
		return nil
	}
}

// OptObjectWith is z.object({...}) for a nested object whose listed members
// are required (the others are optional, null rejected like .optional()).
func OptObjectWith(schema OptionSchema, required ...string) OptionCheck {
	inner := OptObject(schema)
	return func(path string, v interface{}) error {
		obj, ok := asOptionObject(v)
		if !ok {
			return typeIssue(path, "object", v)
		}
		for _, key := range required {
			if member, present := obj[key]; !present || member == nil {
				// Let the member's own check word the failure when present (null).
				if check, known := schema[key]; known {
					if err := check(path+"."+key, member); err != nil {
						return err
					}
				}
				return typeIssue(path+"."+key, "value", nil)
			}
		}
		return inner(path, v)
	}
}
