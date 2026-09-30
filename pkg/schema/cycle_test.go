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
