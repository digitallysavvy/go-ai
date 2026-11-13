package jsonexact

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// jsNumberSentinel prefixes the string that stands in for a number beyond
// the double range while Decode retries (a NUL never starts a JSON number).
const jsNumberSentinel = "\x00go-ai:jsnumber:"

// Decode is json.Unmarshal, except that a number beyond the double range
// (1e400) decodes as +Inf or -Inf, as JSON.parse reads it, wherever it lands
// in an interface position: an interface{} target or field, or a value of a
// map or slice of interface values, at any depth. encoding/json fails the
// whole decode on such a number. A number held by a typed numeric field
// still fails (zod's z.number() rejects Infinity), with the strict decode's
// error. A number that underflows reads as 0 in both. A payload with a
// forbidden prototype key fails first with the TS JSONParseError
// (ParseError), as TS secureJsonParse does.
func Decode(data []byte, v interface{}) error {
	if err := ParseError(data); err != nil {
		return err
	}
	return decodeJS(data, v)
}

// decodeJS is Decode without the prototype check.
func decodeJS(data []byte, v interface{}) error {
	err := json.Unmarshal(data, v)
	if err == nil || !isOverflowError(err) {
		return err
	}
	return decodeNonFinite(data, v, err)
}

// isOverflowError reports whether err is encoding/json's error for a number
// literal beyond the double range.
func isOverflowError(err error) bool {
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return false
	}
	s, ok := strings.CutPrefix(typeErr.Value, "number ")
	if !ok {
		return false
	}
	_, inf := overflowValue(s)
	return inf
}

// overflowValue parses a JSON number literal and reports whether it lies
// beyond the double range, returning the matching infinity.
func overflowValue(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err == nil || !math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

type jsLiteral struct {
	start, end int
	value      float64
}

// overflowLiterals lists the number tokens of valid JSON text (outside
// strings) that lie beyond the double range.
func overflowLiterals(data []byte) []jsLiteral {
	var out []jsLiteral
	for i := 0; i < len(data); {
		c := data[i]
		switch {
		case c == '"':
			i++
			for i < len(data) && data[i] != '"' {
				if data[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(data) && strings.IndexByte("0123456789.eE+-", data[j]) >= 0 {
				j++
			}
			if f, ok := overflowValue(string(data[i:j])); ok {
				out = append(out, jsLiteral{start: i, end: j, value: f})
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// decodeNonFinite retries the decode with each out-of-range literal written
// as an indexed sentinel string, then sets each sentinel found in an
// interface position to its infinity. A sentinel no walk reaches (a
// json.RawMessage, an unexported field) gets its literal back for a second
// pass, which keeps raw values byte for byte; a sentinel in a typed field
// (string, number) returns the strict decode's error origErr.
func decodeNonFinite(data []byte, v interface{}, origErr error) error {
	lits := overflowLiterals(data)
	if len(lits) == 0 {
		return origErr
	}
	active := make([]bool, len(lits))
	for i := range active {
		active[i] = true
	}
	for pass := 0; pass < 2; pass++ {
		var buf bytes.Buffer
		last := 0
		for i, lit := range lits {
			if !active[i] {
				continue
			}
			buf.Write(data[last:lit.start])
			buf.WriteString(`"\u0000go-ai:jsnumber:`)
			buf.WriteString(strconv.Itoa(i))
			buf.WriteByte('"')
			last = lit.end
		}
		buf.Write(data[last:])
		if err := json.Unmarshal(buf.Bytes(), v); err != nil {
			var typeErr *json.UnmarshalTypeError
			if errors.As(err, &typeErr) && strings.HasPrefix(typeErr.Value, "number") {
				// A real number type error the overflow hid (a
				// fraction in an integer field): callers such as
				// UnmarshalChunk retry on it.
				return err
			}
			return origErr
		}
		w := &sentinelWalk{lits: lits, found: make([]bool, len(lits)), seen: map[uintptr]bool{}}
		w.value(reflect.ValueOf(v), 0)
		if w.bad {
			return origErr
		}
		missing := false
		for i := range lits {
			if active[i] && !w.found[i] {
				active[i] = false
				missing = true
			}
		}
		if !missing {
			return nil
		}
	}
	return origErr
}

type sentinelWalk struct {
	lits  []jsLiteral
	found []bool
	bad   bool
	seen  map[uintptr]bool
	count int
}

// index returns the literal index a sentinel string names.
func (w *sentinelWalk) index(s string) (int, bool) {
	rest, ok := strings.CutPrefix(s, jsNumberSentinel)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	if err != nil || i < 0 || i >= len(w.lits) {
		return 0, false
	}
	return i, true
}

func (w *sentinelWalk) value(v reflect.Value, depth int) {
	if depth > maxDepth || !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || w.seen[v.Pointer()] {
			return
		}
		w.seen[v.Pointer()] = true
		w.value(v.Elem(), depth+1)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		e := v.Elem()
		if e.Kind() == reflect.String {
			if i, ok := w.index(e.String()); ok && v.CanSet() && reflect.TypeOf(w.lits[i].value).AssignableTo(v.Type()) {
				v.Set(reflect.ValueOf(w.lits[i].value))
				w.found[i] = true
				w.count++
			}
			return
		}
		switch e.Kind() {
		case reflect.Map, reflect.Pointer, reflect.Slice:
			w.value(e, depth+1)
		case reflect.Struct, reflect.Array:
			cp := reflect.New(e.Type()).Elem()
			cp.Set(e)
			before := w.count
			w.value(cp, depth+1)
			if w.count != before && v.CanSet() {
				v.Set(cp)
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if sf := t.Field(i); sf.IsExported() || sf.Anonymous {
				w.value(v.Field(i), depth+1)
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		iter := v.MapRange()
		for iter.Next() {
			val := iter.Value()
			cp := reflect.New(val.Type()).Elem()
			cp.Set(val)
			before := w.count
			w.value(cp, depth+1)
			if w.count != before {
				v.SetMapIndex(iter.Key(), cp)
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := 0; i < v.Len(); i++ {
			w.value(v.Index(i), depth+1)
		}
	case reflect.String:
		if _, ok := w.index(v.String()); ok {
			w.bad = true
		}
	}
}
