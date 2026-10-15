package providerutils

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestParseProviderOptions(t *testing.T) {
	schema := OptionSchema{
		"n":    OptNumber,
		"s":    OptString,
		"e":    OptEnum("a", "b"),
		"rec":  OptRecord(OptKeyNumeric, OptNumber),
		"null": OptNullable(OptBool),
	}
	if got, err := ParseProviderOptions("p", map[string]interface{}{}, schema); got != nil || err != nil {
		t.Fatalf("absent key: %v %v", got, err)
	}
	if got, err := ParseProviderOptions("p", map[string]interface{}{"p": nil}, schema); got != nil || err != nil {
		t.Fatalf("null key: %v %v", got, err)
	}
	ok := map[string]interface{}{"n": 1, "s": "x", "e": "a", "rec": map[string]interface{}{"1": 2.5, "": 1}, "null": nil, "extra": []int{1}}
	if _, err := ParseProviderOptions("p", map[string]interface{}{"p": ok}, schema); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	bad := []interface{}{
		"str",
		map[string]interface{}{"n": "1"},
		map[string]interface{}{"n": nil},
		map[string]interface{}{"s": 1},
		map[string]interface{}{"e": "c"},
		map[string]interface{}{"rec": map[string]interface{}{"x": 1}},
		map[string]interface{}{"rec": map[string]interface{}{"1": "y"}},
		map[string]interface{}{"rec": []interface{}{}},
	}
	for i, b := range bad {
		_, err := ParseProviderOptions("p", map[string]interface{}{"p": b}, schema)
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "providerOptions" || invalid.Message != "invalid p provider options" || invalid.Cause == nil {
			t.Fatalf("case %d: error = %#v", i, err)
		}
	}
	if err := OptStringMax(3)("x", strings.Repeat("a", 4)); err == nil {
		t.Fatal("OptStringMax should fail over limit")
	}
	one := 1
	if err := OptInt(&one, nil)("x", 1.5); err == nil {
		t.Fatal("OptInt should reject fractions")
	}
}
