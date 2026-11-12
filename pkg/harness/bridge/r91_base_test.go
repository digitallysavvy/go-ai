package bridge_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// B6: public-key bridge authentication (TS c849d37dac). Ports of
// harness/src/utils/bridge-auth.test.ts, bridge-auth-protocol.test.ts,
// bridge/bridge-auth.test.ts and the bridge-web-socket.test.ts auth cases.

const r91Challenge = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func r91publicKey(t *testing.T, kp bridge.BridgeAuthKeyPair) ed25519.PublicKey {
	t.Helper()
	pub, err := bridge.ParseBridgeAuthPublicKey(kp.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func r91proof(t *testing.T, kp bridge.BridgeAuthKeyPair, challengeFrame string) string {
	t.Helper()
	priv, err := bridge.ParseBridgeAuthPrivateKey(kp.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := bridge.CreateBridgeAuthProof(priv, challengeFrame)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

// TS createBridgeAuthKeyPair: "creates distinct single-line base64url key pairs".
func TestR91baseCreateBridgeAuthKeyPair(t *testing.T) {
	first, second := bridge.CreateBridgeAuthKeyPair(), bridge.CreateBridgeAuthKeyPair()
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(first.PublicKey) {
		t.Fatalf("public key %q", first.PublicKey)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(first.PrivateKey) {
		t.Fatalf("private key is not single-line base64url")
	}
	if first.PrivateKey == second.PrivateKey || first.PublicKey == second.PublicKey {
		t.Fatal("key pairs are not distinct")
	}
	// The private key is PKCS#8 DER (Node createPrivateKey format 'der', type 'pkcs8').
	der, err := base64.RawURLEncoding.DecodeString(first.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatal(err)
	}
	if !key.(ed25519.PrivateKey).Public().(ed25519.PublicKey).Equal(r91publicKey(t, first)) {
		t.Fatal("public key does not match the private key")
	}
}

// TS parseBridgeAuthPrivateKey and parseBridgeAuthPublicKey.
func TestR91baseParseBridgeAuthKeys(t *testing.T) {
	if _, err := bridge.ParseBridgeAuthPrivateKey("nope"); err == nil || err.Error() != "Invalid bridge authentication private key." {
		t.Fatalf("malformed: %v", err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.ParseBridgeAuthPrivateKey(base64.RawURLEncoding.EncodeToString(der)); err == nil ||
		err.Error() != "Bridge authentication requires an Ed25519 private key." {
		t.Fatalf("non-Ed25519: %v", err)
	}
	for _, pk := range []string{"", "not-a-key", strings.Repeat("A", 44)} {
		if _, err := bridge.ParseBridgeAuthPublicKey(pk); err == nil ||
			err.Error() != "The bridge requires a valid Ed25519 public key for authentication." {
			t.Fatalf("public key %q: %v", pk, err)
		}
	}
	r91publicKey(t, bridge.CreateBridgeAuthKeyPair())
}

// TS bridge-auth-protocol.test.ts and createBridgeAuthProof.
func TestR91baseBridgeAuthProtocol(t *testing.T) {
	frame := bridge.SerializeBridgeAuthChallengeFrame(r91Challenge)
	if frame != `{"type":"bridge-auth-challenge","challenge":"`+r91Challenge+`"}` {
		t.Fatalf("challenge frame %s", frame)
	}
	kp := bridge.CreateBridgeAuthKeyPair()
	proof := r91proof(t, kp, frame)
	m := regexp.MustCompile(`^\{"type":"bridge-auth-proof","signature":"([A-Za-z0-9_-]{86})"\}$`).FindStringSubmatch(proof)
	if m == nil {
		t.Fatalf("proof frame %s", proof)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(m[1])
	if !ed25519.Verify(r91publicKey(t, kp), []byte("ai-sdk-harness-bridge-auth-v1\x00"+r91Challenge), sig) {
		t.Fatal("signature is not over the domain-separated challenge")
	}

	priv, _ := bridge.ParseBridgeAuthPrivateKey(kp.PrivateKey)
	a64 := strings.Repeat("a", 64)
	for _, text := range []string{
		`{"type":"bridge-hello"}`,
		`{"type":"bridge-auth-challenge","challenge":"` + strings.Repeat("A", 64) + `"}`,
		`{"type":"bridge-auth-challenge","challenge":"` + strings.Repeat("a", 63) + `"}`,
		`{"challenge":"` + a64 + `","type":"bridge-auth-challenge"}`,
		`{"type":"bridge-auth-challenge","challenge":"` + a64 + `","extra":1}`,
		` {"type":"bridge-auth-challenge","challenge":"` + a64 + `"}`,
	} {
		if _, err := bridge.CreateBridgeAuthProof(priv, text); err == nil || err.Error() != "Invalid bridge authentication challenge." {
			t.Fatalf("%s: %v", text, err)
		}
	}
}

// TS bridge/bridge-auth.test.ts (authenticateBridgeConnection).
func TestR91baseVerifyBridgeAuthProof(t *testing.T) {
	kp := bridge.CreateBridgeAuthKeyPair()
	pub := r91publicKey(t, kp)
	first, second := bridge.NewBridgeAuthChallenge(), bridge.NewBridgeAuthChallenge()
	if first == second || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(first) {
		t.Fatalf("challenges %q %q", first, second)
	}
	proof := []byte(r91proof(t, kp, bridge.SerializeBridgeAuthChallengeFrame(first)))
	if !bridge.VerifyBridgeAuthProof(pub, first, proof, false) {
		t.Fatal("valid proof rejected")
	}
	other := []byte(r91proof(t, bridge.CreateBridgeAuthKeyPair(), bridge.SerializeBridgeAuthChallengeFrame(first)))
	sig85 := `{"type":"bridge-auth-proof","signature":"` + strings.Repeat("A", 85) + `"}`
	for name, tc := range map[string]struct {
		frame  []byte
		binary bool
	}{
		"replayed from another challenge": {[]byte(r91proof(t, kp, bridge.SerializeBridgeAuthChallengeFrame(second))), false},
		"signed by another key":           {other, false},
		"binary frame":                    {proof, true},
		"oversized frame":                 {[]byte(strings.Repeat("a", 1025)), false},
		"command":                         {[]byte(`{"type":"start"}`), false},
		"short signature":                 {[]byte(sig85), false},
		"padded signature":                {[]byte(strings.TrimSuffix(sig85, `"}`) + `="}`), false},
	} {
		if bridge.VerifyBridgeAuthProof(pub, first, tc.frame, tc.binary) {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// Host side against a bridge that authenticates with the public key: the
// endpoint carries no token, the proof is answered on every connect, and
// bridge-hello follows it.
func TestR91baseConnectAuthenticatesWithPublicKey(t *testing.T) {
	kp := bridge.CreateBridgeAuthKeyPair()
	var mu sync.Mutex
	var frames []string
	srv := bridgetest.NewServer(bridgetest.Options{
		PublicKey: kp.PublicKey,
		OnFrame: func(ft string) {
			mu.Lock()
			frames = append(frames, ft)
			mu.Unlock()
		},
	})
	t.Cleanup(srv.Close)
	if strings.Contains(srv.URL(), "token") {
		t.Fatalf("URL %q carries a token", srv.URL())
	}
	hellos := 0
	connect := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{
		AuthPrivateKey:   kp.PrivateKey,
		OpenRetryTimeout: 2 * time.Second,
		OnHello:          func(*bridge.Hello) { hellos++ },
	})
	for _, reason := range []bridge.ConnectReason{bridge.ConnectReasonOpen, bridge.ConnectReasonReconnect} {
		conn, err := connect(bridge.WithConnectReason(context.Background(), reason))
		if err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		_ = conn.Close()
	}
	if hellos != 2 || len(srv.HandshakeHeaders()) != 2 {
		t.Fatalf("hellos = %d, accepted = %d", hellos, len(srv.HandshakeHeaders()))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(frames) != 0 {
		t.Fatalf("frames after the proof = %v, want none", frames)
	}
}

// TS "does not retry when the bridge rejects the proof".
func TestR91baseConnectRejectedProofIsNotRetried(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: bridge.CreateBridgeAuthKeyPair().PublicKey})
	t.Cleanup(srv.Close)
	connect := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{
		AuthPrivateKey:   bridge.CreateBridgeAuthKeyPair().PrivateKey,
		OpenRetryTimeout: 20 * time.Second,
	})
	start := time.Now()
	_, err := connect(context.Background())
	var unauthorized *bridge.UnauthorizedError
	if !errors.As(err, &unauthorized) || err.Error() != "The harness bridge rejected the WebSocket connection as unauthorized (code 1008)." {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("rejected proof was retried")
	}
}

// TS "does not retry when the bridge sends no valid challenge".
func TestR91baseConnectWithoutChallengeIsNotRetried(t *testing.T) {
	kp := bridge.CreateBridgeAuthKeyPair()
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true}) // sends bridge-hello first
	t.Cleanup(srv.Close)
	connect := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{
		AuthPrivateKey:   kp.PrivateKey,
		OpenRetryTimeout: 20 * time.Second,
	})
	_, err := connect(context.Background())
	var authErr *bridge.AuthenticationError
	if !errors.As(err, &authErr) || err.Error() != "The harness bridge did not send a valid authentication challenge." {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), kp.PrivateKey) {
		t.Fatal("error carries the private key")
	}
	if n := len(srv.HandshakeHeaders()); n != 1 {
		t.Fatalf("connections = %d, want 1", n)
	}
}

// TS "rejects an invalid private key before connecting".
func TestR91baseConnectInvalidPrivateKey(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
	t.Cleanup(srv.Close)
	connect := bridge.NewBridgeConnectFunc(srv.Endpoint(), bridge.BridgeConnectOptions{AuthPrivateKey: "invalid", OpenRetryTimeout: time.Second})
	if _, err := connect(context.Background()); err == nil || err.Error() != "Invalid bridge authentication private key." {
		t.Fatalf("err = %v", err)
	}
	if _, err := bridge.Dial(context.Background(), srv.Endpoint(), bridge.DialOptions{AuthPrivateKey: "invalid"}); err == nil {
		t.Fatal("Dial accepted an invalid key")
	}
	if n := len(srv.HandshakeHeaders()); n != 0 {
		t.Fatalf("connections = %d, want 0", n)
	}
}

// TS "answers the challenge with a proof before accepting bridge-hello": the
// proof is the only frame the host sends, and it verifies.
func TestR91baseDialSendsOnlyTheProofBeforeHello(t *testing.T) {
	kp := bridge.CreateBridgeAuthKeyPair()
	pub := r91publicKey(t, kp)
	got := make(chan []string, 1)
	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		_ = websocket.Message.Send(ws, bridge.SerializeBridgeAuthChallengeFrame(r91Challenge))
		var proof string
		_ = websocket.Message.Receive(ws, &proof)
		_ = websocket.Message.Send(ws, `{"type":"bridge-hello","state":"waiting"}`)
		_ = ws.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		sent := []string{proof}
		for {
			var more string
			if websocket.Message.Receive(ws, &more) != nil {
				break
			}
			sent = append(sent, more)
		}
		got <- sent
	}))
	t.Cleanup(ts.Close)
	conn, err := bridge.Dial(context.Background(), harness.PortEndpoint{URL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/"},
		bridge.DialOptions{AuthPrivateKey: kp.PrivateKey, WaitForHello: true, HelloTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sent := <-got
	_ = conn.Close()
	if len(sent) != 1 || !bridge.VerifyBridgeAuthProof(pub, r91Challenge, []byte(sent[0]), false) {
		t.Fatalf("host sent %q", sent)
	}
}

// Launch with AuthPublicKey passes only the public key into the sandbox and
// leaves the endpoint unchanged; a legacy token is neither in the env nor
// in the URL.
func TestR91baseLaunchPassesOnlyThePublicKey(t *testing.T) {
	kp := bridge.CreateBridgeAuthKeyPair()
	sandbox := bridgetest.NewSandbox()
	proc := bridgetest.NewProcess()
	var env map[string]string
	sandbox.SetSpawn(func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		env = opts.Env
		return proc, nil
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
	}()
	launched, err := bridge.Launch(context.Background(), bridge.LaunchOptions{
		Sandbox:         sandbox,
		Command:         "node bridge.mjs",
		Port:            4319,
		AuthPublicKey:   kp.PublicKey,
		Token:           "tok",
		Env:             map[string]string{bridge.EnvChannelToken: "smuggled", "KEEP": "1"},
		StartupTimeout:  time.Second,
		ResolveEndpoint: fixedResolver(4319),
	})
	if err != nil {
		t.Fatal(err)
	}
	if env[bridge.EnvAuthPublicKey] != kp.PublicKey || env[bridge.EnvWSPort] != "4319" || env["KEEP"] != "1" {
		t.Fatalf("env = %v", env)
	}
	if _, ok := env[bridge.EnvChannelToken]; ok {
		t.Fatalf("env carries a channel token: %v", env)
	}
	for k, v := range env {
		if strings.Contains(v, kp.PrivateKey) {
			t.Fatalf("env %s carries the private key", k)
		}
	}
	if launched.Endpoint.URL != "ws://127.0.0.1:4319/" {
		t.Fatalf("Endpoint.URL = %q", launched.Endpoint.URL)
	}
}

func TestR91baseWarnMintBridgeTokenIgnored(t *testing.T) {
	var got []string
	warn := func(s string) { got = append(got, s) }
	bridge.WarnMintBridgeTokenIgnored("codex", nil, warn)
	bridge.WarnMintBridgeTokenIgnored("codex", func(string) string { return "t" }, warn)
	if len(got) != 1 || got[0] != "The codex harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair." {
		t.Fatalf("warnings = %q", got)
	}
}
