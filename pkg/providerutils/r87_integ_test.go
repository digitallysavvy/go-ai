package providerutils

import (
	"net/http"
	"testing"
)

// TS extractResponseHeaders is Object.fromEntries([...response.headers]):
// fetch yields each Set-Cookie line as its own entry (the last one wins) and
// joins every other repeated header with ", " (Node 24:
// {"set-cookie":"b=2","x-a":"1, 2"}).
func TestR87integExtractHeadersSetCookieLastLine(t *testing.T) {
	got := ExtractHeaders(http.Header{"Set-Cookie": {"a=1", "b=2"}, "X-A": {"1", "2"}})
	if got["Set-Cookie"] != "b=2" || got["X-A"] != "1, 2" {
		t.Errorf("ExtractHeaders = %#v", got)
	}
	if FetchHeaderEntry("set-cookie", []string{"a=1", "b=2"}) != "b=2" || FetchHeaderEntry("x", nil) != "" {
		t.Error("FetchHeaderEntry")
	}
}
