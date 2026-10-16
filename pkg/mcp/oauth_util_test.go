package mcp

import "testing"

func TestCheckOAuthResourceAllowedNormalizesDotSegments(t *testing.T) {
	cases := []struct {
		requested, configured string
		want                  bool
	}{
		{"https://h/api/users", "https://h/api", true},
		{"https://h/api/../admin", "https://h/api", false},
		{"https://h/api/%2e%2e/admin", "https://h/api", false},
		{"https://h/api/./users", "https://h/api", true},
		{"https://h/x", "https://h/a/../", true},
		{"https://h/api123", "https://h/api", false},
	}
	for _, tc := range cases {
		got, err := checkOAuthResourceAllowed(tc.requested, tc.configured)
		if err != nil || got != tc.want {
			t.Errorf("check(%q,%q) = %v,%v want %v", tc.requested, tc.configured, got, err, tc.want)
		}
	}
}
