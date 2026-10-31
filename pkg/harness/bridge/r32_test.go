package bridge

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// formatBridgeError JSON.stringifies a wire object without a string message;
// the text keeps the wire key order.
func TestCreateBridgeErrorHandlerKeepsWireKeyOrder(t *testing.T) {
	part, err := harness.DecodeStreamPart([]byte(`{"type":"error","error":{"z":1,"a":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	CreateBridgeErrorHandler("h", "s")(part.(*harness.ErrorPart))
	os.Stderr = orig
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), `bridge emitted an error frame: {"z":1,"a":2}`) {
		t.Fatalf("got %q", out)
	}
}

// safeParseJSON rejects a __proto__ key: the frame carries no seq and fails
// validation; a hello with the key is not a hello; a ready line with it is not
// ready.
func TestBridgeFramesRejectForbiddenPrototypeKey(t *testing.T) {
	if _, seq, err := DecodeOutbound([]byte(`{"type":"bridge-thread","threadId":"t","seq":3,"__proto__":{}}`)); err == nil || seq != nil {
		t.Fatalf("outbound: seq=%v err=%v", seq, err)
	}
	if _, ok := parseHello([]byte(`{"type":"bridge-hello","__proto__":{}}`)); ok {
		t.Fatal("hello with __proto__ must be ignored")
	}
	if _, ok := parseHello([]byte(`{"type":"bridge-hello"}`)); !ok {
		t.Fatal("control hello must parse")
	}
	if _, err := DecodeReady([]byte(`{"type":"bridge-ready","port":1,"__proto__":{}}`)); err == nil {
		t.Fatal("ready with __proto__ must be rejected")
	}
}

// TS safeParseJSON with harnessV1BridgeHelloSchema drops a hello whose
// optional fields have the wrong type, and Dial keeps waiting.
func TestParseHelloRejectsWrongFieldTypes(t *testing.T) {
	for _, frame := range []string{
		`{"type":"bridge-hello","state":5}`,
		`{"type":"bridge-hello","lastSeq":"3"}`,
		`{"type":"bridge-hello","capabilities":{"experimental_userMessageResponses":"yes"}}`,
	} {
		if _, ok := parseHello([]byte(frame)); ok {
			t.Errorf("parseHello(%s) accepted a frame that fails the TS schema", frame)
		}
	}
	if h, ok := parseHello([]byte(`{"type":"bridge-hello","state":"waiting","lastSeq":2}`)); !ok || h.State != "waiting" {
		t.Fatalf("valid hello: ok=%v h=%+v", ok, h)
	}
}
