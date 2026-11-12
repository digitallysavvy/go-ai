package elevenlabs

import (
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// normalizeProviderOptions returns providerOptions with Go-typed literals
// (map[string]bool, map[string]float64, []map[string]interface{}, int64 and
// the other integer kinds) read through their JSON form
// (providerutils.NormalizeOptionsJSON), the shape TS parseProviderOptions
// sees and the map-based readers expect. Values holding the package's typed
// option structs (SpeechModelOptions, SpeechVoiceSettings,
// []PronunciationDictionaryLocator, ...) are kept as they are: the readers
// accept them directly, and the speech structs have no JSON tags. The input
// is not modified.
func normalizeProviderOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	out, _ := normalizeOptionValue(providerOptions).(map[string]interface{})
	if out == nil {
		return providerOptions
	}
	return out
}

func normalizeOptionValue(v interface{}) interface{} {
	if !holdsStruct(reflect.ValueOf(v)) {
		return providerutils.NormalizeOptionsJSON(map[string]interface{}{"v": v})["v"]
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = normalizeOptionValue(iter.Value().Interface())
		}
		return out
	}
	// A struct, a pointer to one, or a typed struct slice: kept.
	return v
}

// holdsStruct reports whether v is or contains a struct (other than through
// an interface holding only JSON-shaped values).
func holdsStruct(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Struct:
		return true
	case reflect.Ptr, reflect.Interface:
		return !rv.IsNil() && holdsStruct(rv.Elem())
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			if holdsStruct(iter.Value()) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		if k := rv.Type().Elem().Kind(); k == reflect.Struct || (k == reflect.Ptr && rv.Type().Elem().Elem().Kind() == reflect.Struct) {
			return true
		}
		for i := 0; i < rv.Len(); i++ {
			if holdsStruct(rv.Index(i)) {
				return true
			}
		}
	}
	return false
}
