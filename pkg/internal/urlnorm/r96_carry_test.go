package urlnorm

import "testing"

// new URL(path, base) for a scheme-relative reference keeps only the base
// scheme (Node 24); an opaque-path base still fails.
func TestR96carryResolvePathSchemeRelative(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"ws://h:80/z", `\/x:`, "ws://x/"},
		{"foo:/a/b", "///", "foo:///"},
		{"file:///C:/d/e", `\/`, "file:///"},
		{"https://u:p@a.com:8443/z", "/\t/x/y?q", "https://x/y?q"},
		{"http://h/", "//h2:81/p", "http://h2:81/p"},
	}
	for _, c := range cases {
		base, err := Parse(c.base)
		if err != nil {
			t.Fatal(err)
		}
		u, err := base.ResolvePath(c.path)
		if err != nil || u.String() != c.want {
			t.Errorf("ResolvePath(%q, %q) = %v, %v; want %s", c.path, c.base, u, err, c.want)
		}
	}
	opaque, _ := Parse("foo:opaque")
	if _, err := opaque.ResolvePath("//x"); err == nil {
		t.Error("an opaque-path base resolved a scheme-relative reference")
	}
}
