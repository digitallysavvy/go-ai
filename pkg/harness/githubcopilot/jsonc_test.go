package githubcopilot

import "testing"

func TestParseJSONC(t *testing.T) {
	// Mirrors "reads the last logged-in account from the macOS Keychain"'s
	// config fixture: a leading `//` comment and a trailing comma before `}`.
	text := `
		// This file is managed by Copilot CLI.
		{
		  "last_logged_in_user": {
		    "host": "https://enterprise.example/",
		    "login": "last-user",
		  },
		  "logged_in_users": [
		    { "host": "https://github.com", "login": "first-user" }
		  ],
		  "copilotTokens": {
		    "https://enterprise.example:last-user": "plaintext-token"
		  },
		}
	`
	got, err := ParseJSONC([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	user, ok := got["last_logged_in_user"].(map[string]any)
	if !ok || user["login"] != "last-user" {
		t.Errorf("last_logged_in_user = %v", got["last_logged_in_user"])
	}
	tokens, ok := got["copilotTokens"].(map[string]any)
	if !ok || tokens["https://enterprise.example:last-user"] != "plaintext-token" {
		t.Errorf("copilotTokens = %v", got["copilotTokens"])
	}
}

func TestParseJSONC_BlockComments(t *testing.T) {
	text := `{ /* leading */ "a": 1 /* trailing */ }`
	got, err := ParseJSONC([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != float64(1) {
		t.Errorf("a = %v", got["a"])
	}
}

func TestParseJSONC_CommentLikeStringsPreserved(t *testing.T) {
	text := `{ "url": "https://example.com", "note": "a, b" }`
	got, err := ParseJSONC([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://example.com" {
		t.Errorf("url = %v", got["url"])
	}
	if got["note"] != "a, b" {
		t.Errorf("note = %v", got["note"])
	}
}

func TestParseJSONC_TrailingCommaInArray(t *testing.T) {
	got, err := ParseJSONC([]byte(`{"items": [1, 2, 3,]}`))
	if err != nil {
		t.Fatal(err)
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 3 {
		t.Errorf("items = %v", got["items"])
	}
}

// Mirrors "ignores malformed native configuration".
func TestParseJSONC_Malformed(t *testing.T) {
	if _, err := ParseJSONC([]byte("{ invalid")); err == nil {
		t.Error("expected an error for malformed JSONC")
	}
}

func TestParseJSONC_ScannerParity(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"unterminated block comment", "{\"a\":1} /* oops", false},
		{"unterminated block comment star", "{\"a\":1} /* oops *", false},
		{"empty block comment", "{\"a\":1} /**/", true},
		{"BOM", "\ufeff{\"a\":1}", true},
		{"nbsp and form feed", "{\u00a0\"a\"\f:\u30001}", true},
		{"CR ends line comment", "{\"a\":1 // c\r,\"b\":2}", true},
		{"BOM inside string kept", "{\"a\":\"\ufeff\"}", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseJSONC([]byte(c.in))
			if (err == nil) != c.ok {
				t.Fatalf("ParseJSONC(%q) err = %v, want ok=%v", c.in, err, c.ok)
			}
		})
	}
}
