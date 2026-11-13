package urlnorm

import "testing"

// Expected values from Node 24 (`x.protocol = p`, `x.pathname = p`,
// `new URL(p, base).href`); a generated differential of 10,000+ setter and
// resolve cases matched after these fixes (review round 95).
func TestR95carrySetters(t *testing.T) {
	type tc struct{ kind, base, value, want string }
	for _, c := range []tc{
		{"protocol", "https://h/x", "w\tss", "wss://h/x"},
		{"protocol", "http://h:80/", "\nws:", "ws://h/"},
		{"pathname", "foo:/a/b", "c", "foo:/c"},
		{"pathname", "foo:/a/b", "", "foo:/"},
		{"pathname", "foo:/a/b", `\x`, `foo:/\x`},
		{"pathname", "foo:/a/b", "..", "foo:/"},
		{"resolve", "file:///C:/y", "/x", "file:///C:/x"},
		{"resolve", "file:///C:/y", `\x?q`, "file:///C:/x?q"},
		{"resolve", "file:///C:/y", "/D:/x", "file:///D:/x"},
		{"resolve", "file://host/c:/y", "/x", "file://host/c:/x"},
	} {
		u, err := Parse(c.base)
		if err != nil {
			t.Fatal(err)
		}
		switch c.kind {
		case "protocol":
			u.SetProtocol(c.value)
		case "pathname":
			u.SetPathname(c.value)
		case "resolve":
			if u, err = u.ResolvePath(c.value); err != nil {
				t.Errorf("new URL(%q, %q): %v", c.value, c.base, err)
				continue
			}
		}
		if got := u.String(); got != c.want {
			t.Errorf("%s %q %q = %s, want %s", c.kind, c.base, c.value, got, c.want)
		}
	}
	// "/\t/x" is the authority form "//x" once tabs are dropped: not a
	// path-absolute reference.
	base, _ := Parse("https://h/a")
	if _, err := base.ResolvePath("/\t/x"); err == nil {
		t.Error(`ResolvePath("/\t/x") must fail like "//x"`)
	}
}
