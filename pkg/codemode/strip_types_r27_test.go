package codemode

import "testing"

// Cases checked against Node 24 stripTypeScriptTypes (wrapped in the run
// package's function body); a rejected snippet comes back unchanged.
func TestStripR27NodeParity(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a()<T>(x); return 1;", "a()   (x); return 1;"},
		{"a[0]<T>(x); return 1;", "a[0]   (x); return 1;"},
		{"a!<T>(x); return 1;", "a    (x); return 1;"},
		{"a?.<T>(x); return 1;", "a?.   (x); return 1;"},
		{"a.b?.<T>(x); return 1;", "a.b?.   (x); return 1;"},
		{"a!<T>`x`; return 1;", "a    `x`; return 1;"},
		{"a?.b!<T>(x); return 1;", "a?.b    (x); return 1;"},
		{"new A!<T>(x); return 1;", "new A    (x); return 1;"},
		{"(a)<T>(x); return 1;", "(a)   (x); return 1;"},
		{"a<T>(x)<U>(y); return 1;", "a   (x)   (y); return 1;"},
		{"a<T>(x)!<U>(y); return 1;", "a   (x)    (y); return 1;"},
		{"var abstract = 1; abstract\nclass A {}; return 1;", "var abstract = 1; abstract\nclass A {}; return 1;"},
		{"x = y as A<B<C>>>1; return x;", "x = y           >1; return x;"},
		{"x = y as A<B>>1; return x;", "x = y        >1; return x;"},
		{"x = y as A<B>>=1; return x;", "x = y        >=1; return x;"},
		{"x = y as A<B>>>=1; return x;", "x = y        >>=1; return x;"},
		{"x = y as A<B<C>>>>1; return x;", "x = y           >>1; return x;"},
		{"x = f(1) as T < y;", "x = f(1) as T < y;"},
		{"x = f(1) as T < y; return x;", "x = f(1) as T < y; return x;"},
		{"class A { type = 1; as = 2; declare = 3; readonly = 4; static = 5; }\nreturn A;", "class A { type = 1; as = 2; declare = 3; readonly = 4; static = 5; }\nreturn A;"},
		{"class A { readonly = 1; x: number; }\nreturn A;", "class A { readonly = 1; x        ; }\nreturn A;"},
		{"class A { declare; readonly; override = 1 }\nreturn A;", "class A { declare; readonly; override = 1 }\nreturn A;"},
		{"class A { readonly\nx = 1 }\nreturn A;", "class A { readonly\nx = 1 }\nreturn A;"},
		{"class A { public\nx = 1 }\nreturn A;", "class A { public\nx = 1 }\nreturn A;"},
		{"class A { readonly?: number; get<T>(): T { return 1 } }\nreturn A;", "class A { readonly         ; get   ()    { return 1 } }\nreturn A;"},
		{"x = a as // line\n   any as // line\n   string; return x;", "x = a           \n                 \n         ; return x;"},
		{"let x = a satisfies B as // line\n   C; return x;", "let x = a                       \n    ; return x;"},
		{"let x = y as unknown /* c */ as\n   T[]; return x;", "let x = y                      \n      ; return x;"},
	} {
		got, _ := stripTypeScriptAnnotations(tc.src)
		if got != tc.want {
			t.Errorf("strip(%q)\n got  %q\n want %q", tc.src, got, tc.want)
		}
	}
}
