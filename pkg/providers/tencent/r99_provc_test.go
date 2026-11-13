package tencent

import (
	"context"
	"testing"
)

// TS drops a caller header when `name.toLowerCase() in protocolHeaders`,
// which also matches the object's inherited "constructor" / "__proto__".
func TestR99provcCloudAPIDropsInheritedKeyHeaders(t *testing.T) {
	srv, calls := cloudServer(t, `{"Response":{"TaskId":"task-1"}}`)
	p := New(Config{VodBaseURL: srv.URL})
	headers := map[string]string{"Constructor": "c", "__proto__": "p", "X-Other": "o"}
	if _, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, headers, 1794120000); err != nil {
		t.Fatal(err)
	}
	h := calls()[0].headers
	if h.Get("Constructor") != "" || h.Get("__proto__") != "" || h.Get("X-Other") != "o" {
		t.Fatalf("headers = %v", h)
	}
}
