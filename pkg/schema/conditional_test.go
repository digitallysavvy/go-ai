package schema

import "testing"

func TestConditionalContainsDependencies(t *testing.T) {
	const d2020 = "https://json-schema.org/draft/2020-12/schema"
	obj := func(kv ...interface{}) map[string]interface{} {
		m := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	ifThenElse := obj(
		"if", obj("properties", obj("kind", obj("const", "a")), "required", []interface{}{"kind"}),
		"then", obj("required", []interface{}{"x"}),
		"else", obj("required", []interface{}{"y"}),
	)
	runValidateCases(t, []validateCase{
		{name: "if then ok", schema: ifThenElse, value: obj("kind", "a", "x", 1.0)},
		{name: "if then fails", schema: ifThenElse, value: obj("kind", "a"), wantErr: true, errSubstr: `"then" schema`},
		{name: "else ok", schema: ifThenElse, value: obj("kind", "b", "y", 1.0)},
		{name: "else fails", schema: ifThenElse, value: obj("kind", "b"), wantErr: true, errSubstr: `"else" schema`},
		{name: "if without branches", schema: obj("if", obj("type", "string")), value: 1.0},
		{name: "if false else", schema: obj("if", false, "else", obj("type", "string")), value: 1.0, wantErr: true, errSubstr: `"else"`},

		{name: "contains ok", schema: obj("contains", obj("type", "string")), value: []interface{}{1.0, "a"}},
		{name: "contains none", schema: obj("contains", obj("type", "string")), value: []interface{}{1.0}, wantErr: true, errSubstr: "contains"},
		{name: "contains empty", schema: obj("contains", obj("type", "string")), value: []interface{}{}, wantErr: true, errSubstr: "contains"},
		{name: "minContains ignored in draft-07", schema: obj("contains", obj("type", "string"), "minContains", 2.0), value: []interface{}{"a"}},
		{name: "minContains 2020", schema: obj("$schema", d2020, "contains", obj("type", "string"), "minContains", 2.0), value: []interface{}{"a"}, wantErr: true, errSubstr: "at least 2"},
		{name: "maxContains 2020", schema: obj("$schema", d2020, "contains", obj("type", "string"), "maxContains", 1.0), value: []interface{}{"a", "b"}, wantErr: true, errSubstr: "no more than 1"},
		{name: "minContains 0 2020", schema: obj("$schema", d2020, "contains", obj("type", "string"), "minContains", 0.0), value: []interface{}{1.0}},

		{name: "dependencies array ok", schema: obj("dependencies", obj("a", []interface{}{"b"})), value: obj("a", 1.0, "b", 2.0)},
		{name: "dependencies array fails", schema: obj("dependencies", obj("a", []interface{}{"b"})), value: obj("a", 1.0), wantErr: true, errSubstr: "dependencies"},
		{name: "dependencies absent trigger", schema: obj("dependencies", obj("a", []interface{}{"b"})), value: obj("c", 1.0)},
		{name: "dependencies schema fails", schema: obj("dependencies", obj("a", obj("required", []interface{}{"b"}))), value: obj("a", 1.0), wantErr: true, errSubstr: "dependencies"},
		{name: "dependentSchemas ignored in draft-07", schema: obj("dependentSchemas", obj("a", obj("required", []interface{}{"b"}))), value: obj("a", 1.0)},
		{name: "dependentSchemas 2020", schema: obj("$schema", d2020, "dependentSchemas", obj("a", obj("required", []interface{}{"b"}))), value: obj("a", 1.0), wantErr: true, errSubstr: "dependentSchemas"},
	})
}
