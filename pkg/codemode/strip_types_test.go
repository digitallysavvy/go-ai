package codemode

import "testing"

func TestStripTypeScriptAnnotations_VariableTypeAnnotation(t *testing.T) {
	got := stripTypeScriptAnnotations("const value: number = 7; return { value };")
	// Whitespace between the identifier and `=` is not preserved when a
	// type annotation is stripped (cosmetic only; the result is still
	// valid, equivalent JavaScript).
	want := "const value= 7; return { value };"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripTypeScriptAnnotations_InterfaceAndSatisfies(t *testing.T) {
	src := "interface Item { value: number }\nconst item = { value: 12 } satisfies Item;\nreturn item;"
	got := stripTypeScriptAnnotations(src)
	want := "\nconst item = { value: 12 } ;\nreturn item;"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripTypeScriptAnnotations_TypeAlias(t *testing.T) {
	got := stripTypeScriptAnnotations("type Foo = { a: number };\nreturn 1;")
	want := "\nreturn 1;"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripTypeScriptAnnotations_LeavesPlainObjectLiteralsAlone(t *testing.T) {
	src := "const o = { a: 1, b: 2 }; return o.a + o.b;"
	got := stripTypeScriptAnnotations(src)
	if got != src {
		t.Fatalf("plain JS object literal should be untouched: got %q, want %q", got, src)
	}
}

func TestStripTypeScriptAnnotations_LeavesPropertyNamedTypeAlone(t *testing.T) {
	src := "const o = { type: 'x' }; return o.type;"
	got := stripTypeScriptAnnotations(src)
	if got != src {
		t.Fatalf("object property named 'type' should be untouched: got %q, want %q", got, src)
	}
}

func TestStripTypeScriptAnnotations_LeavesStringAndCommentContentAlone(t *testing.T) {
	src := "const s = 'interface Foo {}'; // interface Bar { x: number }\nreturn s;"
	got := stripTypeScriptAnnotations(src)
	if got != src {
		t.Fatalf("string/comment contents should be untouched: got %q, want %q", got, src)
	}
}

func TestStripTypeScriptAnnotations_DoesNotTouchAsPropertyName(t *testing.T) {
	src := "const o = { as: 1 }; return o.as;"
	got := stripTypeScriptAnnotations(src)
	if got != src {
		t.Fatalf("'as' as a plain property name should be untouched: got %q, want %q", got, src)
	}
}
