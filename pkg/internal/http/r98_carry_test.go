package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS combineHeaders then withUserAgentSuffix (normalizeHeaders): the combined
// record is lower-cased in entry order, so a later spelling of a header wins
// over an earlier one that differs only in case.
func TestR98carryMergeHeadersFoldsCaseLastWins(t *testing.T) {
	got := MergeHeaders(map[string]string{"anthropic-version": "sdk", "x-api-key": "k"}, map[string]string{"Anthropic-Version": "user"})
	if len(got) != 2 || got["Anthropic-Version"] != "user" {
		t.Fatalf("MergeHeaders = %v, want one anthropic-version entry, the caller's", got)
	}
	got = MergeHeaders(map[string]string{"Prefer": "wait"}, map[string]string{"prefer": "x"}, nil)
	if len(got) != 1 || got["prefer"] != "x" {
		t.Fatalf("MergeHeaders = %v, want {prefer: x}", got)
	}

	var seen []string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seen = append(seen, r.Header.Get("Anthropic-Version")+"|"+r.Header.Get("Authorization"))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Headers: providerutils.WithUserAgentSuffix(MergeHeaders(
		map[string]string{"anthropic-version": "sdk", "authorization": "Bearer sdk"},
		map[string]string{"Anthropic-Version": "user", "Authorization": "Bearer user"},
	), "ai-sdk/test")})
	for i := 0; i < 20; i++ {
		if _, err := c.Do(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range seen {
		if s != "user|Bearer user" {
			t.Fatalf("sent %q, want the caller's headers on every request", s)
		}
	}
}

// TS response.statusText is the status line's reason phrase; the provider
// failed-response handlers fall back to it. provc CR r98 part 1.
func TestR98carryResponseStatusTextIsReasonPhrase(t *testing.T) {
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		conn, buf, err := w.(stdhttp.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close() //nolint:errcheck
		code := "418 Custom Phrase"
		if r.URL.Path == "/ok" {
			code = "200 Fine Thanks"
		}
		_, _ = buf.WriteString("HTTP/1.1 " + code + "\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
		_ = buf.Flush()
	})
	srv.Start()
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL})

	resp, err := c.Do(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/ok"})
	if err != nil || resp.StatusText != "Fine Thanks" {
		t.Fatalf("Do = %+v, %v; want StatusText Fine Thanks", resp, err)
	}
	var out map[string]interface{}
	err = c.DoJSON(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/x"}, &out)
	statusErr, ok := err.(*HTTPStatusError)
	if !ok || statusErr.StatusText != "Custom Phrase" {
		t.Fatalf("DoJSON error = %#v, want StatusText Custom Phrase", err)
	}
	if _, err := c.DoJSONResponse(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/x"}, &out); err.(*HTTPStatusError).StatusText != "Custom Phrase" {
		t.Fatalf("DoJSONResponse error = %#v", err)
	}
	if _, err := c.DoStream(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/x"}); err.(*HTTPStatusError).StatusText != "Custom Phrase" {
		t.Fatalf("DoStream error = %#v", err)
	}
	if got := httpStatusText(&stdhttp.Response{StatusCode: 404, Status: "404"}); got != "Not Found" {
		t.Fatalf("httpStatusText without a phrase = %q, want the standard text", got)
	}
}
