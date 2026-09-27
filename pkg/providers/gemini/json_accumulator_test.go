package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from ai/packages/google/src/google-json-accumulator.test.ts
// (TS `GoogleJSONAccumulator`, commits 5036db8/a2609df/cfca634).

func strPtr(s string) *string   { return &s }
func numPtr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool      { return &b }

func TestGoogleJSONAccumulator_FlatPaths(t *testing.T) {
	t.Run("simple string arg with willContinue", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston"), WillContinue: boolPtr(true)},
		})
		assert.Equal(t, map[string]interface{}{"location": "Boston"}, result.CurrentJSON)
		// No closing quote: the string is left "open" for the next
		// willContinue chunk (see emitLeaf).
		assert.Equal(t, `{"location":"Boston`, result.TextDelta)
	})

	t.Run("continue a string arg across multiple chunks", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston"), WillContinue: boolPtr(true)},
		})
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr(", MA")},
		})
		assert.Equal(t, map[string]interface{}{"location": "Boston, MA"}, result.CurrentJSON)
		assert.Equal(t, ", MA", result.TextDelta)
	})

	t.Run("complete string arg (no willContinue)", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston")},
		})
		assert.Equal(t, map[string]interface{}{"location": "Boston"}, result.CurrentJSON)
		assert.Equal(t, `{"location":"Boston"`, result.TextDelta)
	})

	t.Run("number arg", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.brightness", NumberValue: numPtr(50)},
		})
		assert.Equal(t, map[string]interface{}{"brightness": float64(50)}, result.CurrentJSON)
		assert.Equal(t, `{"brightness":50`, result.TextDelta)
	})

	t.Run("boolean arg", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.enabled", BoolValue: boolPtr(true)},
		})
		assert.Equal(t, map[string]interface{}{"enabled": true}, result.CurrentJSON)
		assert.Equal(t, `{"enabled":true`, result.TextDelta)
	})

	t.Run("null arg", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.nickname", HasNullValue: true},
		})
		assert.Equal(t, map[string]interface{}{"nickname": nil}, result.CurrentJSON)
		assert.Equal(t, `{"nickname":null`, result.TextDelta)
	})

	t.Run("multiple args with commas between them", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		first := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.brightness", NumberValue: numPtr(50)},
		})
		assert.Equal(t, `{"brightness":50`, first.TextDelta)

		second := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.enabled", BoolValue: boolPtr(true)},
		})
		assert.Equal(t, map[string]interface{}{"brightness": float64(50), "enabled": true}, second.CurrentJSON)
		assert.Equal(t, `,"enabled":true`, second.TextDelta)
	})

	t.Run("multiple args in a single call", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.brightness", NumberValue: numPtr(50)},
			{JSONPath: "$.enabled", BoolValue: boolPtr(false)},
			{JSONPath: "$.nickname", HasNullValue: true},
		})
		assert.Equal(t, map[string]interface{}{
			"brightness": float64(50),
			"enabled":    false,
			"nickname":   nil,
		}, result.CurrentJSON)
		assert.Equal(t, `{"brightness":50,"enabled":false,"nickname":null`, result.TextDelta)
	})

	t.Run("escape special characters in continued strings", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.query", StringValue: strPtr(`Boston "Lo`), WillContinue: boolPtr(true)},
		})
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.query", StringValue: strPtr(`gan"`)},
		})
		assert.Equal(t, map[string]interface{}{"query": `Boston "Logan"`}, result.CurrentJSON)
		assert.Equal(t, `gan\"`, result.TextDelta)
	})

	t.Run("skip args with empty jsonPath after stripping $. prefix", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.", StringValue: strPtr("ignored")},
		})
		assert.Equal(t, map[string]interface{}{}, result.CurrentJSON)
		assert.Equal(t, "", result.TextDelta)
	})

	t.Run("skip args with no resolvable value", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.something"},
		})
		assert.Equal(t, map[string]interface{}{}, result.CurrentJSON)
		assert.Equal(t, "", result.TextDelta)
	})

	t.Run("empty textDelta for empty partialArgs slice", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{})
		assert.Equal(t, map[string]interface{}{}, result.CurrentJSON)
		assert.Equal(t, "", result.TextDelta)
	})
}

func TestGoogleJSONAccumulator_NestedPaths(t *testing.T) {
	t.Run("build nested object from dotted jsonPath", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.name", StringValue: strPtr("Lasagna")},
		})
		assert.Equal(t, map[string]interface{}{
			"recipe": map[string]interface{}{"name": "Lasagna"},
		}, result.CurrentJSON)
		assert.Equal(t, `{"recipe":{"name":"Lasagna"`, result.TextDelta)
	})

	t.Run("build nested object with array from indexed jsonPath", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		amount := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[0].amount", StringValue: strPtr("16 oz")},
		})
		assert.Equal(t, `{"recipe":{"ingredients":[{"amount":"16 oz"`, amount.TextDelta)

		name := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[0].name", StringValue: strPtr("Lasagna noodles")},
		})
		assert.Equal(t, `,"name":"Lasagna noodles"`, name.TextDelta)
		assert.Equal(t, map[string]interface{}{
			"recipe": map[string]interface{}{
				"ingredients": []interface{}{
					map[string]interface{}{"amount": "16 oz", "name": "Lasagna noodles"},
				},
			},
		}, name.CurrentJSON)
	})

	t.Run("accumulate multiple array elements across chunks", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		var deltas []string

		r := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[0].amount", StringValue: strPtr("16 oz")},
		})
		deltas = append(deltas, r.TextDelta)

		r = acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[0].name", StringValue: strPtr("Noodles")},
		})
		deltas = append(deltas, r.TextDelta)

		r = acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[1].amount", StringValue: strPtr("1 lb")},
		})
		deltas = append(deltas, r.TextDelta)
		assert.Equal(t, `},{"amount":"1 lb"`, r.TextDelta)

		r = acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[1].name", StringValue: strPtr("Beef")},
		})
		deltas = append(deltas, r.TextDelta)
		assert.Equal(t, `,"name":"Beef"`, r.TextDelta)

		finalJSON, closingDelta := acc.Finalize()
		deltas = append(deltas, closingDelta)

		joined := ""
		for _, d := range deltas {
			joined += d
		}
		assert.Equal(t, finalJSON, joined)
	})

	t.Run("handle string continuation on nested paths", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		start := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.steps[0]", StringValue: strPtr("Preheat oven"), WillContinue: boolPtr(true)},
		})
		assert.Equal(t, `{"recipe":{"steps":["Preheat oven`, start.TextDelta)

		cont := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.steps[0]", StringValue: strPtr(" to 375°F.")},
		})
		assert.Equal(t, " to 375°F.", cont.TextDelta)
		assert.Equal(t, map[string]interface{}{
			"recipe": map[string]interface{}{
				"steps": []interface{}{"Preheat oven to 375°F."},
			},
		}, cont.CurrentJSON)
	})

	t.Run("mixed nested and flat paths", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		loc := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston")},
		})
		assert.Equal(t, `{"location":"Boston"`, loc.TextDelta)

		details := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.details.zip", StringValue: strPtr("02101")},
		})
		assert.Equal(t, `,"details":{"zip":"02101"`, details.TextDelta)
		assert.Equal(t, map[string]interface{}{
			"location": "Boston",
			"details":  map[string]interface{}{"zip": "02101"},
		}, details.CurrentJSON)

		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `}}`, closingDelta)
		assert.Equal(t, `{"location":"Boston","details":{"zip":"02101"}}`, finalJSON)
	})

	t.Run("array elements that are direct string values", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		first := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.steps[0]", StringValue: strPtr("Step one")},
		})
		assert.Equal(t, `{"steps":["Step one"`, first.TextDelta)

		second := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.steps[1]", StringValue: strPtr("Step two")},
		})
		assert.Equal(t, `,"Step two"`, second.TextDelta)
		assert.Equal(t, map[string]interface{}{
			"steps": []interface{}{"Step one", "Step two"},
		}, second.CurrentJSON)
	})

	t.Run("deeply nested paths", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		result := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.a.b.c.d", StringValue: strPtr("deep")},
		})
		assert.Equal(t, `{"a":{"b":{"c":{"d":"deep"`, result.TextDelta)

		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `}}}}`, closingDelta)
		assert.Equal(t, `{"a":{"b":{"c":{"d":"deep"}}}}`, finalJSON)
	})
}

func TestGoogleJSONAccumulator_Finalize(t *testing.T) {
	t.Run("closing delta for a continued string", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston"), WillContinue: boolPtr(true)},
		})
		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `"}`, closingDelta)
		assert.Equal(t, `{"location":"Boston"}`, finalJSON)
	})

	t.Run("closing delta for a complete string", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston")},
		})
		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `}`, closingDelta)
		assert.Equal(t, `{"location":"Boston"}`, finalJSON)
	})

	t.Run("closing delta for multiple args", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.brightness", NumberValue: numPtr(50)},
			{JSONPath: "$.enabled", BoolValue: boolPtr(true)},
		})
		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `}`, closingDelta)
		assert.Equal(t, `{"brightness":50,"enabled":true}`, finalJSON)
	})

	t.Run("closing delta for continued string with continuation", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr("Boston"), WillContinue: boolPtr(true)},
		})
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.location", StringValue: strPtr(", MA")},
		})
		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `"}`, closingDelta)
		assert.Equal(t, `{"location":"Boston, MA"}`, finalJSON)
	})

	t.Run("empty accumulator", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		finalJSON, closingDelta := acc.Finalize()
		assert.Equal(t, `{}`, closingDelta)
		assert.Equal(t, `{}`, finalJSON)
	})

	t.Run("finalize nested structure to proper JSON", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.ingredients[0].name", StringValue: strPtr("Noodles")},
		})
		acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.name", StringValue: strPtr("Lasagna")},
		})
		finalJSON, _ := acc.Finalize()

		tree, err := decodeOrderedJSON([]byte(finalJSON))
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
			"recipe": map[string]interface{}{
				"ingredients": []interface{}{
					map[string]interface{}{"name": "Noodles"},
				},
				"name": "Lasagna",
			},
		}, toPlainJSON(tree))
	})
}

func TestGoogleJSONAccumulator_ConcatenationInvariant(t *testing.T) {
	t.Run("flat args", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		var deltas []string

		r := acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.brightness", NumberValue: numPtr(50)}})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.enabled", BoolValue: boolPtr(true)}})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.name", StringValue: strPtr("test")}})
		deltas = append(deltas, r.TextDelta)

		finalJSON, closingDelta := acc.Finalize()
		deltas = append(deltas, closingDelta)

		joined := ""
		for _, d := range deltas {
			joined += d
		}
		assert.Equal(t, finalJSON, joined)

		tree, err := decodeOrderedJSON([]byte(finalJSON))
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
			"brightness": float64(50),
			"enabled":    true,
			"name":       "test",
		}, toPlainJSON(tree))
	})

	t.Run("nested args", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		var deltas []string

		steps := []PartialArg{
			{JSONPath: "$.recipe.ingredients[0].amount", StringValue: strPtr("16 oz")},
			{JSONPath: "$.recipe.ingredients[0].name", StringValue: strPtr("Noodles")},
			{JSONPath: "$.recipe.ingredients[1].amount", StringValue: strPtr("1 lb")},
			{JSONPath: "$.recipe.ingredients[1].name", StringValue: strPtr("Beef")},
			{JSONPath: "$.recipe.name", StringValue: strPtr("Lasagna")},
		}
		for _, s := range steps {
			r := acc.ProcessPartialArgs([]PartialArg{s})
			deltas = append(deltas, r.TextDelta)
		}

		r := acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.steps[0]", StringValue: strPtr("Preheat"), WillContinue: boolPtr(true)},
		})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.steps[0]", StringValue: strPtr(" oven.")},
		})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{
			{JSONPath: "$.recipe.steps[1]", StringValue: strPtr("Cook.")},
		})
		deltas = append(deltas, r.TextDelta)

		finalJSON, closingDelta := acc.Finalize()
		deltas = append(deltas, closingDelta)

		joined := ""
		for _, d := range deltas {
			joined += d
		}
		assert.Equal(t, finalJSON, joined)
	})

	t.Run("willContinue strings", func(t *testing.T) {
		acc := NewGoogleJSONAccumulator()
		var deltas []string

		r := acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.location", StringValue: strPtr("Bos"), WillContinue: boolPtr(true)}})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.location", StringValue: strPtr("ton")}})
		deltas = append(deltas, r.TextDelta)
		r = acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.count", NumberValue: numPtr(42)}})
		deltas = append(deltas, r.TextDelta)

		finalJSON, closingDelta := acc.Finalize()
		deltas = append(deltas, closingDelta)

		joined := ""
		for _, d := range deltas {
			joined += d
		}
		assert.Equal(t, finalJSON, joined)
	})
}
