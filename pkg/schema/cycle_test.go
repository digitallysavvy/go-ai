package schema

import (
	"strings"
	"testing"
	"time"
)

// TestSchemaCyclesDoNotOverflowStack covers SC2 item 1: a $ref cycle that
// never consumes any data (via allOf/anyOf/oneOf/not, or a $ref straight
// back to the document root) must be rejected with a normal Go error, not
// crash the process with a Go fatal ("goroutine stack exceeds ... bytes",
// which recover() cannot catch). Each case must return promptly; if the
// guard regresses, these tests hang/crash rather than fail cleanly.
func TestSchemaCyclesDoNotOverflowStack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		schema    map[string]interface{}
		errSubstr string
	}{
		{
			name: "allOf self-cycle",
			schema: map[string]interface{}{
				"$defs": map[string]interface{}{
					"A": map[string]interface{}{
						"allOf": []interface{}{
							map[string]interface{}{"$ref": "#/$defs/A"},
						},
					},
				},
				"$ref": "#/$defs/A",
			},
			errSubstr: "circular reference",
		},
		{
			name: "not self-cycle",
			schema: map[string]interface{}{
				"$defs": map[string]interface{}{
					"A": map[string]interface{}{
						"not": map[string]interface{}{"$ref": "#/$defs/A"},
					},
				},
				"$ref": "#/$defs/A",
			},
			errSubstr: "circular reference",
		},
		{
			name: "anyOf self-cycle",
			schema: map[string]interface{}{
				"$defs": map[string]interface{}{
					"A": map[string]interface{}{
						"anyOf": []interface{}{
							map[string]interface{}{"$ref": "#/$defs/A"},
						},
					},
				},
				"$ref": "#/$defs/A",
			},
			errSubstr: "circular reference",
		},
		{
			name: "oneOf self-cycle",
			schema: map[string]interface{}{
				"$defs": map[string]interface{}{
					"A": map[string]interface{}{
						"oneOf": []interface{}{
							map[string]interface{}{"$ref": "#/$defs/A"},
						},
					},
				},
				"$ref": "#/$defs/A",
			},
			errSubstr: "circular reference",
		},
		{
			name: "root #/ cycle via allOf",
			schema: map[string]interface{}{
				"allOf": []interface{}{
					map[string]interface{}{"$ref": "#/"},
				},
			},
			errSubstr: "circular reference",
		},
		{
			name: "mutual A -> B -> A cycle via allOf",
			schema: map[string]interface{}{
				"$defs": map[string]interface{}{
					"A": map[string]interface{}{
						"allOf": []interface{}{
							map[string]interface{}{"$ref": "#/$defs/B"},
						},
					},
					"B": map[string]interface{}{
						"allOf": []interface{}{
							map[string]interface{}{"$ref": "#/$defs/A"},
						},
					},
				},
				"$ref": "#/$defs/A",
			},
			errSubstr: "circular reference",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			done := make(chan error, 1)
			go func() {
				done <- NewJSONSchema(tc.schema).Validate(map[string]interface{}{})
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("Validate() = nil, want a circular reference error")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tc.errSubstr)
				}
			case <-timeoutCh(t):
				t.Fatal("Validate() did not return -- likely unbounded recursion")
			}

			// ApplyDefaults must also return (unchanged value), not hang/crash.
			s := NewSimpleJSONSchema(tc.schema)
			doneDefaults := make(chan interface{}, 1)
			go func() {
				doneDefaults <- ApplyDefaults(map[string]interface{}{}, s)
			}()
			select {
			case out := <-doneDefaults:
				m, ok := out.(map[string]interface{})
				if !ok || len(m) != 0 {
					t.Fatalf("ApplyDefaults() = %#v, want the input value unchanged", out)
				}
			case <-timeoutCh(t):
				t.Fatal("ApplyDefaults() did not return -- likely unbounded recursion")
			}
		})
	}
}

// TestSchemaCycleGuardAllowsDataDrivenRecursion is the negative counterpart
// to TestSchemaCyclesDoNotOverflowStack: a schema that recurses through
// actual data (a classic recursive "tree" schema, and a $ref straight back
// to the document root reached only through a property) must keep working
// -- the cycle guard must not false-positive on legitimate recursion.
func TestSchemaCycleGuardAllowsDataDrivenRecursion(t *testing.T) {
	t.Parallel()

	treeSchema := map[string]interface{}{
		"$defs": map[string]interface{}{
			"Node": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value": map[string]interface{}{"type": "string"},
					"children": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"$ref": "#/$defs/Node"},
					},
				},
				"required": []interface{}{"value"},
			},
		},
		"$ref": "#/$defs/Node",
	}

	validTree := map[string]interface{}{
		"value": "root",
		"children": []interface{}{
			map[string]interface{}{
				"value": "child-1",
				"children": []interface{}{
					map[string]interface{}{"value": "grandchild-1"},
				},
			},
			map[string]interface{}{"value": "child-2"},
		},
	}
	if err := NewJSONSchema(treeSchema).Validate(validTree); err != nil {
		t.Fatalf("recursive tree schema rejected valid data: %v", err)
	}

	invalidTree := map[string]interface{}{
		"value": "root",
		"children": []interface{}{
			map[string]interface{}{
				// missing required "value" three levels deep.
				"children": []interface{}{
					map[string]interface{}{"value": "grandchild"},
				},
			},
		},
	}
	if err := NewJSONSchema(treeSchema).Validate(invalidTree); err == nil {
		t.Fatal("recursive tree schema accepted data missing a required field three levels deep")
	}

	rootSelfRef := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"self": map[string]interface{}{"$ref": "#/"},
		},
	}
	nested := map[string]interface{}{
		"self": map[string]interface{}{
			"self": map[string]interface{}{},
		},
	}
	if err := NewJSONSchema(rootSelfRef).Validate(nested); err != nil {
		t.Fatalf("data-driven root #/ self-reference rejected valid nested data: %v", err)
	}
}

// TestSchemaCycleGuardAllowsSiblingRefReuse covers the false-positive risks
// called out for SC2 item 1: the same non-cyclic $ref used twice at sibling
// positions -- two object properties, and two anyOf branches at the same
// data path -- must validate normally. A regression that failed to reset or
// release the guard between siblings would report a spurious "circular
// reference" here even though neither $ref ever re-enters itself.
func TestSchemaCycleGuardAllowsSiblingRefReuse(t *testing.T) {
	t.Parallel()

	t.Run("same ref in two sibling properties", func(t *testing.T) {
		t.Parallel()
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"A": map[string]interface{}{"type": "string"},
			},
			"type": "object",
			"properties": map[string]interface{}{
				"a": map[string]interface{}{"$ref": "#/$defs/A"},
				"b": map[string]interface{}{"$ref": "#/$defs/A"},
			},
		}
		if err := NewJSONSchema(s).Validate(map[string]interface{}{"a": "x", "b": "y"}); err != nil {
			t.Fatalf("sibling properties sharing a $ref rejected valid data: %v", err)
		}
	})

	t.Run("same ref in two anyOf branches at the same data path", func(t *testing.T) {
		t.Parallel()
		// The first anyOf branch resolves #/$defs/A (type: string) and then
		// applies its own sibling "minLength" constraint that the value
		// fails; the second branch resolves the very same $ref with no
		// extra constraint and must still be tried normally -- the guard
		// must have released "#/$defs/A" once the first branch's
		// validateSchemaValue call returned, cyclic or not.
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"A": map[string]interface{}{"type": "string"},
			},
			"anyOf": []interface{}{
				map[string]interface{}{"$ref": "#/$defs/A", "minLength": 100},
				map[string]interface{}{"$ref": "#/$defs/A"},
			},
		}
		if err := NewJSONSchema(s).Validate("hi"); err != nil {
			t.Fatalf("anyOf: value does not match any subschema (last error: %v); want the second branch (same $ref, no minLength) to match", err)
		}
	})
}

// TestSchemaCycleGuardPropagatesThroughCombinators covers the rest of SC2
// item 1: a genuine circular $ref reached through one branch of an
// anyOf/oneOf/not must surface as an error and must not be swallowed by
// those keywords' normal "did not match this branch, keep going" handling
// (anyOf/oneOf) or turned into an accidental pass ("not": a subschema that
// errored, rather than merely failing to match, does not satisfy "not").
func TestSchemaCycleGuardPropagatesThroughCombinators(t *testing.T) {
	t.Parallel()

	cyclicB := map[string]interface{}{
		"allOf": []interface{}{
			map[string]interface{}{"$ref": "#/$defs/B"},
		},
	}

	t.Run("anyOf stops at a circular branch instead of treating it as a mismatch", func(t *testing.T) {
		t.Parallel()
		// The cyclic branch is listed first so anyOf's early-exit-on-match
		// can never mask it by matching a later, valid branch first.
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"B": cyclicB,
			},
			"anyOf": []interface{}{
				map[string]interface{}{"$ref": "#/$defs/B"},
				map[string]interface{}{"type": "string"},
			},
		}
		done := make(chan error, 1)
		go func() { done <- NewJSONSchema(s).Validate("valid for branch 2") }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("anyOf swallowed a circular-reference branch as an ordinary mismatch and fell through to a later matching branch")
			}
			if !strings.Contains(err.Error(), "circular reference") {
				t.Fatalf("anyOf error = %q, want a circular reference error", err.Error())
			}
		case <-timeoutCh(t):
			t.Fatal("Validate() did not return -- likely unbounded recursion")
		}
	})

	t.Run("oneOf still reports a circular branch even after counting an earlier match", func(t *testing.T) {
		t.Parallel()
		// oneOf (unlike anyOf) evaluates every branch to count matches, so
		// this also exercises that a circular branch encountered *after* a
		// real match still aborts instead of being folded into the match
		// count.
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"B": cyclicB,
			},
			"oneOf": []interface{}{
				map[string]interface{}{"type": "string"},
				map[string]interface{}{"$ref": "#/$defs/B"},
			},
		}
		done := make(chan error, 1)
		go func() { done <- NewJSONSchema(s).Validate("matches branch 1") }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("oneOf swallowed a circular-reference branch instead of reporting it")
			}
			if !strings.Contains(err.Error(), "circular reference") {
				t.Fatalf("oneOf error = %q, want a circular reference error", err.Error())
			}
		case <-timeoutCh(t):
			t.Fatal("Validate() did not return -- likely unbounded recursion")
		}
	})

	t.Run("not of a circular ref is an error, not an accidental pass", func(t *testing.T) {
		t.Parallel()
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"B": cyclicB,
			},
			"not": map[string]interface{}{"$ref": "#/$defs/B"},
		}
		done := make(chan error, 1)
		go func() { done <- NewJSONSchema(s).Validate(map[string]interface{}{}) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal(`"not" treated a circular-reference subschema as "did not match", making Validate() pass -- want an error`)
			}
			if !strings.Contains(err.Error(), "circular reference") {
				t.Fatalf(`"not" error = %q, want a circular reference error (not a "not: value must not match the schema" false pass)`, err.Error())
			}
		case <-timeoutCh(t):
			t.Fatal("Validate() did not return -- likely unbounded recursion")
		}
	})
}

// TestSchemaCycleGuardAllowsDataDrivenRecursionViaOtherEdges extends
// TestSchemaCycleGuardAllowsDataDrivenRecursion to the remaining
// data-consuming edges named in SC2 item 1 -- additionalProperties,
// patternProperties, and prefixItems -- confirming each resets the guard
// (so legitimate recursion through them terminates and validates normally)
// exactly like properties/items already do.
func TestSchemaCycleGuardAllowsDataDrivenRecursionViaOtherEdges(t *testing.T) {
	t.Parallel()

	t.Run("additionalProperties recursion", func(t *testing.T) {
		t.Parallel()
		// A "bag" node whose extra (non-"label") properties are themselves
		// bag nodes, recursing through additionalProperties rather than a
		// named property.
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"Bag": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"label": map[string]interface{}{"type": "string"},
					},
					"additionalProperties": map[string]interface{}{"$ref": "#/$defs/Bag"},
				},
			},
			"$ref": "#/$defs/Bag",
		}
		valid := map[string]interface{}{
			"label": "root",
			"child": map[string]interface{}{
				"label":      "nested",
				"grandchild": map[string]interface{}{"label": "leaf"},
			},
		}
		if err := NewJSONSchema(s).Validate(valid); err != nil {
			t.Fatalf("recursive additionalProperties schema rejected valid data: %v", err)
		}
		invalid := map[string]interface{}{
			"label": "root",
			"child": map[string]interface{}{"label": 123},
		}
		if err := NewJSONSchema(s).Validate(invalid); err == nil {
			t.Fatal("recursive additionalProperties schema accepted a nested type mismatch")
		}
	})

	t.Run("patternProperties recursion", func(t *testing.T) {
		t.Parallel()
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"Bag": map[string]interface{}{
					"type": "object",
					"patternProperties": map[string]interface{}{
						"^node_": map[string]interface{}{"$ref": "#/$defs/Bag"},
					},
				},
			},
			"$ref": "#/$defs/Bag",
		}
		valid := map[string]interface{}{
			"node_a": map[string]interface{}{
				"node_b": map[string]interface{}{},
			},
		}
		if err := NewJSONSchema(s).Validate(valid); err != nil {
			t.Fatalf("recursive patternProperties schema rejected valid data: %v", err)
		}
	})

	t.Run("prefixItems recursion", func(t *testing.T) {
		t.Parallel()
		// A cons-cell-like tuple: [value, rest], where "rest" is either null
		// or another cons cell -- recursing through prefixItems.
		s := map[string]interface{}{
			"$defs": map[string]interface{}{
				"Cons": map[string]interface{}{
					"type":        "array",
					"prefixItems": []interface{}{},
				},
			},
		}
		cons := map[string]interface{}{
			"type": "array",
			"prefixItems": []interface{}{
				map[string]interface{}{"type": "integer"},
				map[string]interface{}{
					"anyOf": []interface{}{
						map[string]interface{}{"type": "null"},
						map[string]interface{}{"$ref": "#/$defs/Cons"},
					},
				},
			},
		}
		s["$defs"].(map[string]interface{})["Cons"] = cons
		s["$ref"] = "#/$defs/Cons"

		valid := []interface{}{1, []interface{}{2, []interface{}{3, nil}}}
		if err := NewJSONSchema(s).Validate(valid); err != nil {
			t.Fatalf("recursive prefixItems schema rejected valid data: %v", err)
		}
		invalid := []interface{}{1, []interface{}{"not an integer", nil}}
		if err := NewJSONSchema(s).Validate(invalid); err == nil {
			t.Fatal("recursive prefixItems schema accepted a nested type mismatch")
		}
	})
}

// timeoutCh returns a channel that fires well before Go's test timeout, so a
// regression to unbounded recursion fails the test with a clear message
// instead of hanging the whole `go test` run (a true stack-overflow
// regression is an uncatchable Go fatal error and would still kill the test
// binary outright, but this guards the case where a future change turns the
// infinite recursion into a merely-very-slow one).
func timeoutCh(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(5 * time.Second)
}
