package provider

import "reflect"

// DecisionQuestionsJSON returns questions with typed Go containers in
// Instructions and Criteria (any slice or array, and any map with string
// keys, such as []string or map[string]string) converted to the
// []interface{} / map[string]interface{} forms the checks and the providers
// read, as their JSON form: TS questions are JSON values. questions itself
// is returned when nothing is converted.
func DecisionQuestionsJSON(questions map[string]DecisionQuestion) map[string]DecisionQuestion {
	var out map[string]DecisionQuestion
	for id, q := range questions {
		instructions, convertedInstructions := decisionJSONValue(q.Instructions)
		criteria, convertedCriteria := decisionJSONValue(q.Criteria)
		if !convertedInstructions && !convertedCriteria {
			continue
		}
		if out == nil {
			out = make(map[string]DecisionQuestion, len(questions))
			for k, v := range questions {
				out[k] = v
			}
		}
		q.Instructions, q.Criteria = instructions, criteria
		out[id] = q
	}
	if out == nil {
		return questions
	}
	return out
}

// decisionJSONValue converts a typed Go container (and named basic types
// inside it) to the generic JSON form, and reports whether it did. A nil
// typed slice or map is JSON null (nil). []interface{} and
// map[string]interface{} are kept as they are (same identity, so the cycle
// check still sees them), as are values with no JSON form here (structs,
// pointers, []byte), which the checks reject as before.
func decisionJSONValue(value interface{}) (interface{}, bool) {
	switch value.(type) {
	case nil, string, bool, float64, []interface{}, map[string]interface{}, []byte:
		return value, false
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, true
		}
		out := make([]interface{}, rv.Len())
		for i := range out {
			out[i], _ = decisionJSONValue(rv.Index(i).Interface())
		}
		return out, true
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value, false
		}
		if rv.IsNil() {
			return nil, true
		}
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()], _ = decisionJSONValue(iter.Value().Interface())
		}
		return out, true
	case reflect.String:
		return rv.String(), true
	case reflect.Bool:
		return rv.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return value, false
}
