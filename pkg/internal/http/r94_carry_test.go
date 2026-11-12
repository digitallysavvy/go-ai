package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

// TS response schemas (zod objects) match keys exactly: a differently cased
// key is unknown and stripped, so DoJSON / DoJSONResponse do not fill the
// field from it.
func TestR94carryDoJSONMatchesKeysExactly(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(`{"ID":"wrong","Data":[{"URL":"x"}],"data":[{"url":"u","URL":"x"}]}`))
	}))
	defer srv.Close()
	type item struct {
		URL string `json:"url"`
	}
	type body struct {
		ID   string `json:"id"`
		Data []item `json:"data"`
	}
	c := NewClient(Config{BaseURL: srv.URL})
	var out body
	if err := c.DoJSON(context.Background(), Request{Method: "GET", Path: "/"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "" || len(out.Data) != 1 || out.Data[0].URL != "u" {
		t.Errorf("DoJSON = %+v, want id empty and data[0].url u", out)
	}
	var out2 body
	if _, err := c.DoJSONResponse(context.Background(), Request{Method: "GET", Path: "/"}, &out2); err != nil || out2.ID != "" || out2.Data[0].URL != "u" {
		t.Errorf("DoJSONResponse = %+v, %v", out2, err)
	}
}
