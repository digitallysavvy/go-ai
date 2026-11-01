package zodlite

import (
	"encoding/json"
	"testing"
)

// r75provbIssues renders the compact zod issue list of value against node
// ("" when it passes).
func r75provbIssues(t *testing.T, node Node, value string) string {
	t.Helper()
	text := IssueText(ValidationMessage(node, value))
	if text == "" {
		return ""
	}
	return CompactJSON(text)
}

// TestR75ProvbChecksAndUnionReporting: the zod 4.4.3 issue JSON of the r75
// additions (Int, Min/Max, Regex, StrictObject, DiscriminatedUnion, Lazy)
// and zod v4 union reporting (a single non-aborted member reports its own
// issues). Expected texts are zod 4.4.3 safeParse output.
func TestR75ProvbChecksAndUnionReporting(t *testing.T) {
	intRange := Object(F("a", Optional(Max(Min(Int, 0), 100))))
	size := Object(F("size", Optional(Union(Enum("1024x1024", "auto"), Regex(`^\d+x\d+$`)))))
	cases := []struct {
		name  string
		node  Node
		value string
		want  string
	}{
		{"int fraction aborts the bounds", intRange, `{"a":1.5}`, `[{"expected":"int","format":"safeint","code":"invalid_type","path":["a"],"message":"Invalid input: expected int, received number"}]`},
		{"max", intRange, `{"a":200}`, `[{"origin":"number","code":"too_big","maximum":100,"inclusive":true,"path":["a"],"message":"Too big: expected number to be <=100"}]`},
		{"min", intRange, `{"a":-3}`, `[{"origin":"number","code":"too_small","minimum":0,"inclusive":true,"path":["a"],"message":"Too small: expected number to be >=0"}]`},
		{"not a number", intRange, `{"a":"x"}`, `[{"expected":"number","code":"invalid_type","path":["a"],"message":"Invalid input: expected number, received string"}]`},
		{"unsafe int then max", intRange, `{"a":1e20}`, `[{"code":"too_big","maximum":9007199254740991,"note":"Integers must be within the safe integer range.","origin":"int","inclusive":true,"path":["a"],"message":"Too big: expected int to be <=9007199254740991"},{"origin":"number","code":"too_big","maximum":100,"inclusive":true,"path":["a"],"message":"Too big: expected number to be <=100"}]`},
		{"in range", intRange, `{"a":100}`, ``},
		{"enum|regex reports the regex only", size, `{"size":"big"}`, `[{"origin":"string","code":"invalid_format","format":"regex","pattern":"/^\\d+x\\d+$/","path":["size"],"message":"Invalid string: must match pattern /^\\d+x\\d+$/"}]`},
		{"enum|regex both aborted", size, `{"size":5}`, `[{"code":"invalid_union","errors":[[{"code":"invalid_value","values":["1024x1024","auto"],"path":[],"message":"Invalid option: expected one of \"1024x1024\"|\"auto\""}],[{"expected":"string","code":"invalid_type","path":[],"message":"Invalid input: expected string, received number"}]],"path":["size"],"message":"Invalid input"}]`},
		{"regex match", size, `{"size":"512x512"}`, ``},
		{"strict object", StrictObject(F("a", Optional(String)), F("b", Optional(Number))), `{"a":2,"c":3,"z":1}`, `[{"expected":"string","code":"invalid_type","path":["a"],"message":"Invalid input: expected string, received number"},{"code":"unrecognized_keys","keys":["c","z"],"path":[],"message":"Unrecognized keys: \"c\", \"z\""}]`},
		{"object union with a nested check issue", Union(Object(F("a", Regex(`^a$`))), Object(F("b", String))), `{"a":"b"}`, `[{"origin":"string","code":"invalid_format","format":"regex","pattern":"/^a$/","path":["a"],"message":"Invalid string: must match pattern /^a$/"}]`},
		{"array element and max", Object(F("x", MaxItems(Array(String), 2))), `{"x":["a",1,"c"]}`, `[{"expected":"string","code":"invalid_type","path":["x",1],"message":"Invalid input: expected string, received number"},{"origin":"array","code":"too_big","maximum":2,"inclusive":true,"path":["x"],"message":"Too big: expected array to have <=2 items"}]`},
		{"discriminated union 4.4", DiscriminatedUnion([]string{"a", "b"}, map[string]Node{"a": Typed("a"), "b": Typed("b")}), `{"type":"c"}`, `[{"code":"invalid_union","errors":[],"note":"No matching discriminator","discriminator":"type","options":["a","b"],"path":["type"],"message":"Invalid discriminator value. Expected 'a' | 'b'"}]`},
		{"discriminated union 4.4 non-object", DiscriminatedUnion([]string{"a"}, map[string]Node{"a": Typed("a")}), `"x"`, `[{"code":"invalid_type","expected":"object","path":[],"message":"Invalid input: expected object, received string"}]`},
	}
	for _, c := range cases {
		if got := r75provbIssues(t, c.node, c.value); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// TestR75ProvbExistingNodesUnchanged: nodes the stream frame schemas
// (anthropic, google) and the other providers use render as before r75: Disc
// keeps the zod 4.1-4.3 discriminator issue, a union of type-failing members
// is one invalid_union, StrictEmptyObject reports sorted keys.
func TestR75ProvbExistingNodesUnchanged(t *testing.T) {
	disc := Disc(map[string]Node{"a": Typed("a")})
	if got := r75provbIssues(t, disc, `{"type":"c"}`); got != `[{"code":"invalid_union","errors":[],"note":"No matching discriminator","discriminator":"type","path":["type"],"message":"Invalid input"}]` {
		t.Errorf("Disc = %s", got)
	}
	if got := r75provbIssues(t, disc, `"x"`); got != `[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received string"}]` {
		t.Errorf("Disc non-object = %s", got)
	}
	u := Union(String, Number)
	if got := r75provbIssues(t, u, `true`); got != `[{"code":"invalid_union","errors":[[{"expected":"string","code":"invalid_type","path":[],"message":"Invalid input: expected string, received boolean"}],[{"expected":"number","code":"invalid_type","path":[],"message":"Invalid input: expected number, received boolean"}]],"path":[],"message":"Invalid input"}]` {
		t.Errorf("Union = %s", got)
	}
	if got := r75provbIssues(t, StrictEmptyObject, `{"b":1,"a":2}`); got != `[{"code":"unrecognized_keys","keys":["a","b"],"path":[],"message":"Unrecognized keys: \"a\", \"b\""}]` {
		t.Errorf("StrictEmptyObject = %s", got)
	}
	// A hand-built issue without Continue (bfl's invalid_format URL) still
	// aborts, so a union reports every member.
	custom := Node(func(v interface{}, present bool, path []interface{}) *Issue {
		return &Issue{Path: path, Code: "invalid_format", Message: "Invalid URL"}
	})
	if got := r75provbIssues(t, Union(custom, Number), `"x"`); got == "" || got[:25] != `[{"code":"invalid_union",` {
		t.Errorf("Union with a non-continue custom issue = %s", got)
	}
}

// TestR75ProvbLazyRecursion: Lazy lets a node refer to itself.
func TestR75ProvbLazyRecursion(t *testing.T) {
	var tree Node
	tree = Object(F("children", Optional(Array(Lazy(func() Node { return tree })))), F("n", Number))
	var v interface{}
	if err := json.Unmarshal([]byte(`{"n":1,"children":[{"n":2,"children":[{"n":"x"}]}]}`), &v); err != nil {
		t.Fatal(err)
	}
	bad := tree(v, true, nil)
	if bad == nil || bad.Error() != "children[0].children[0].n: Invalid input: expected number, received string" {
		t.Fatalf("recursive issue = %v", bad)
	}
}
