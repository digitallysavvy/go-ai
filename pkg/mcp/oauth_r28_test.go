package mcp

import (
	"net/http"
	"strings"
	"testing"
)

func TestDecodeOAuthTokenResponseRejectsUnsafeURLs(t *testing.T) {
	for _, field := range []string{"issuer", "authorization_server", "token_endpoint"} {
		for _, bad := range []string{"javascript://x/%0a", "data:text/plain,a", "not a url"} {
			body := `{"access_token":"a","token_type":"Bearer","` + field + `":"` + bad + `"}`
			resp := &http.Response{StatusCode: 200, Body: nopBody(body), Header: http.Header{}}
			if _, err := decodeOAuthTokenResponse(resp); err == nil {
				t.Errorf("%s=%q accepted", field, bad)
			}
		}
	}
	ok := &http.Response{StatusCode: 200, Header: http.Header{}, Body: nopBody(`{"access_token":"a","token_type":"Bearer","issuer":"https://as.example"}`)}
	if _, err := decodeOAuthTokenResponse(ok); err != nil {
		t.Fatal(err)
	}
}

type strBody struct{ *strings.Reader }

func (strBody) Close() error { return nil }

func nopBody(s string) strBody { return strBody{strings.NewReader(s)} }
