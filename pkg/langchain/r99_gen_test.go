package langchain

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
)

// Round 99 gen tests (TS 2ebc78b654; @langchain/langgraph 1.7.1,
// @langchain/langgraph-sdk 1.14.0).

// GraphInterrupt (langgraph errors.js): `this.interrupts = interrupts ?? []`,
// so a null __interrupt__ gives an empty list while the message is
// JSON.stringify(null) = "null".
func TestR99genGraphInterruptNullIsEmptyList(t *testing.T) {
	events, err := r92genEvents(t, "event: updates\ndata: {\"__interrupt__\":null}\n\n")
	var gi *GraphInterruptError
	if len(events) != 0 || !errors.As(err, &gi) {
		t.Fatalf("events %v, err %v; want a GraphInterrupt", events, err)
	}
	if !reflect.DeepEqual(gi.Interrupts, []interface{}{}) || gi.Error() != "null" {
		t.Fatalf("Interrupts = %#v, message %q; want [] and \"null\"", gi.Interrupts, gi.Error())
	}
}

// SSEDecoder decodes each field value with TextDecoder (ignoreBOM): a
// maximal invalid subpart is one U+FFFD (node: JSON.parse of the decoded
// data `{"a":"\xe2\x82x\xff"}` is {"a":"�x�"}); encoding/json
// alone writes one U+FFFD per invalid byte.
func TestR99genSSEDataInvalidUTF8(t *testing.T) {
	events, err := r92genEvents(t, "event: values\ndata: {\"a\":\"\xe2\x82x\xff\"}\n\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []StreamEvent{{Mode: "values", Data: map[string]interface{}{"a": "�x�"}}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

// HTTPError.fromResponse: `HTTP ${status}: ${await response.text()}`, and
// response.text() drops a leading BOM and replaces invalid bytes (node:
// TextDecoder().decode of "\xef\xbb\xbfbad\xe2\x82" is "bad�").
func TestR99genHTTPErrorBodyDecodedAsText(t *testing.T) {
	server := r94genServer(t, http.StatusBadRequest, "text/plain", "\xef\xbb\xbfbad\xe2\x82", nil)
	_, errs := r94genSendHi(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL}))
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	var httpErr *LangGraphHTTPError
	if !errors.As(errs[0], &httpErr) || httpErr.Error() != "HTTP 400: bad�" {
		t.Fatalf("err = %q, want %q", errs[0], "HTTP 400: bad�")
	}
}
