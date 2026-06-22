package providerutils

import "testing"

func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name string
		url  string
		base string
		want bool
	}{
		{name: "same origin", url: "https://api.example.com/v1/files/1", base: "https://api.example.com/v1/jobs/1", want: true},
		{name: "default https port", url: "https://api.example.com:443/v1/files/1", base: "https://api.example.com/v1/jobs/1", want: true},
		{name: "default http port", url: "http://api.example.com:80/v1/files/1", base: "http://api.example.com/v1/jobs/1", want: true},
		{name: "case insensitive host and scheme", url: "HTTPS://API.EXAMPLE.COM/v1/files/1", base: "https://api.example.com/v1/jobs/1", want: true},
		{name: "different scheme", url: "http://api.example.com/v1/files/1", base: "https://api.example.com/v1/jobs/1"},
		{name: "different host", url: "https://cdn.example.com/file", base: "https://api.example.com/v1/jobs/1"},
		{name: "different port", url: "https://api.example.com:8443/file", base: "https://api.example.com/file"},
		{name: "invalid url", url: "not-a-url", base: "https://api.example.com/file"},
		{name: "invalid base", url: "https://api.example.com/file", base: "not-a-url"},
		{name: "invalid url port", url: "https://api.example.com:bad/file", base: "https://api.example.com/file"},
		{name: "invalid base port", url: "https://api.example.com/file", base: "https://api.example.com:bad/file"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSameOrigin(tc.url, tc.base); got != tc.want {
				t.Fatalf("IsSameOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
