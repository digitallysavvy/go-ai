package fx

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Node's Buffer#toString('utf8') replaces each maximal ill-formed subsequence
// with one U+FFFD (expectations from Node 24).
func TestR49NodeUTF8String(t *testing.T) {
	for _, c := range [][2]string{
		{"61e28262fff09f98", "61efbfbd62efbfbdefbfbd"},
		{"e08080", "efbfbdefbfbdefbfbd"},
		{"eda080", "efbfbdefbfbdefbfbd"},
		{"f4908080", "efbfbdefbfbdefbfbdefbfbd"},
		{"f09f9880", "f09f9880"},
		{"c0af", "efbfbdefbfbd"},
		{"efbfbd", "efbfbd"},
		{"f18080", "efbfbd"},
		{"c2", "efbfbd"},
		{"8080", "efbfbdefbfbd"},
		{"e180c2a9", "efbfbdc2a9"},
	} {
		in, _ := hex.DecodeString(c[0])
		if got := hex.EncodeToString([]byte(nodeUTF8String(in))); got != c[1] {
			t.Errorf("%s: got %s, want %s", c[0], got, c[1])
		}
	}
}

// TS realpath decodes the sandbox's base64 output with Node's UTF-8 decoder,
// so a resolved path that is not valid UTF-8 is read as TS reads it.
func TestR49RemoteOpsRealpathDecodesLikeNode(t *testing.T) {
	sb := newFakeSandbox()
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if opts.Command != "" && len(sb.commandList()) == 1 {
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		}
		return providerutils.SandboxRunResult{Stdout: b64("/workspace/a\xe2\x82b\x00")}, nil
	}
	sb.files["/workspace/a�b"] = "decoded"
	r := newRemoteOps(sb, "/workspace")
	out, err := r.execute(context.Background(), "read", json.RawMessage(`{"file_path":"ab"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "decoded" {
		t.Fatalf("read = %v, want the file at the Node-decoded path", out)
	}
}
