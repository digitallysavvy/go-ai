package bridge_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// r91TSBridgeScript runs the built TS runBridge (@ai-sdk/harness
// dist/bridge/index.js) with an onStart that emits one text delta, and
// prints the bound port.
const r91TSBridgeScript = `
const { runBridge } = await import(process.argv[2]);
const { mkdtempSync } = await import('node:fs');
const { tmpdir } = await import('node:os');
const { join } = await import('node:path');
await runBridge({
  bridgeType: 'r91',
  bridgeStateDir: mkdtempSync(join(tmpdir(), 'r91-bridge-')),
  port: 0,
  onListening: port => console.log(JSON.stringify({ port })),
  async onStart(_start, turn) {
    turn.emit({ type: 'text-delta', id: 'm', delta: 'hi' });
  },
});
`

// The host side against the real, built TS bridge (TS 2ebc78b654): a fresh
// key pair authenticates; another key is rejected with 1008. Runs only when
// GOAI_TS_HARNESS_BRIDGE_JS names the built dist/bridge/index.js and node is
// installed.
func TestR91baseRealTSBridgeAuth(t *testing.T) {
	distJS := os.Getenv("GOAI_TS_HARNESS_BRIDGE_JS")
	if distJS == "" {
		t.Skip("Skipping: GOAI_TS_HARNESS_BRIDGE_JS not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Skipping: node not installed")
	}
	script := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(script, []byte(r91TSBridgeScript), 0o600); err != nil {
		t.Fatal(err)
	}
	kp := bridge.CreateBridgeAuthKeyPair()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, "file://"+distJS)
	cmd.Env = append(os.Environ(), bridge.EnvAuthPublicKey+"="+kp.PublicKey)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	port := 0
	sc := bufio.NewScanner(stdout)
	for port == 0 && sc.Scan() {
		var line struct{ Port int }
		if json.Unmarshal(sc.Bytes(), &line) == nil {
			port = line.Port
		}
	}
	if port == 0 {
		t.Fatal("bridge did not report its port")
	}
	go func() {
		for sc.Scan() {
			_ = sc.Text() // drain the bridge's stdout
		}
	}()
	endpoint := harness.PortEndpoint{URL: "ws://127.0.0.1:" + strconv.Itoa(port) + "/"}

	var hello *bridge.Hello
	conn, err := bridge.NewBridgeConnectFunc(endpoint, bridge.BridgeConnectOptions{
		AuthPrivateKey: kp.PrivateKey, OpenRetryTimeout: 10 * time.Second,
		OnHello: func(h *bridge.Hello) { hello = h },
	})(ctx)
	if err != nil {
		t.Fatalf("authenticated connect: %v", err)
	}
	if hello == nil || hello.State != "waiting" {
		t.Fatalf("hello = %+v", hello)
	}
	if err := conn.Send([]byte(`{"type":"start","prompt":"x"}`)); err != nil {
		t.Fatal(err)
	}
	var sawDelta bool
	for i := 0; i < 10 && !sawDelta; i++ {
		msg, err := conn.Receive()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		sawDelta = strings.Contains(string(msg), `"delta":"hi"`)
	}
	if !sawDelta {
		t.Fatal("no text delta after start")
	}
	_ = conn.Close()

	_, err = bridge.NewBridgeConnectFunc(endpoint, bridge.BridgeConnectOptions{
		AuthPrivateKey: bridge.CreateBridgeAuthKeyPair().PrivateKey, OpenRetryTimeout: 10 * time.Second,
	})(ctx)
	var unauthorized *bridge.UnauthorizedError
	if !errors.As(err, &unauthorized) || unauthorized.Code != 1008 {
		t.Fatalf("wrong key: err = %v", err)
	}
}
