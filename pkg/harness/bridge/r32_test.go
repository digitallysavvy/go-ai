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
