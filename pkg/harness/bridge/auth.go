package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Public-key bridge authentication (TS c849d37dac,
// harness/src/utils/bridge-auth.ts, bridge-auth-protocol.ts and
// bridge/bridge-auth.ts). Each session gets a fresh Ed25519 key pair. The
// sandbox receives only the public key (EnvAuthPublicKey); the private key
// stays on the host. On every connection the bridge sends a random challenge,
// the host answers with a signature, and the bridge sends bridge-hello only
// after verifying it. A shared token in the URL or the sandbox env would let
// any sandbox code take over the bridge.

// EnvAuthPublicKey is the environment variable the in-sandbox bridge reads
// its authentication public key from (TS BRIDGE_AUTH_PUBLIC_KEY_ENV).
const EnvAuthPublicKey = "BRIDGE_AUTH_PUBLIC_KEY"

// AuthTimeout is how long the bridge waits for the host's proof before it
// terminates the connection (TS BRIDGE_AUTH_TIMEOUT_MS).
const AuthTimeout = 10 * time.Second

// AuthMaxFrameBytes is the largest proof frame the bridge reads (TS
// BRIDGE_AUTH_MAX_FRAME_BYTES).
const AuthMaxFrameBytes = 1024

const bridgeAuthMessagePrefix = "ai-sdk-harness-bridge-auth-v1\x00"

// Authentication frames are matched against their exact canonical
// serialization instead of being parsed as JSON, as in TS: an exact match
// rejects any frame with unexpected fields or encodings.
var (
	challengeFrameRegexp = regexp.MustCompile(`^\{"type":"bridge-auth-challenge","challenge":"([a-f0-9]{64})"\}$`)
	proofFrameRegexp     = regexp.MustCompile(`^\{"type":"bridge-auth-proof","signature":"([A-Za-z0-9_-]{86})"\}$`)
	publicKeyRegexp      = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// BridgeAuthKeyPair is a per-session bridge authentication key pair (TS
// BridgeAuthKeyPair).
type BridgeAuthKeyPair struct {
	// PrivateKey is the host-only PKCS#8 DER private key, base64url encoded.
	// Never pass it into the sandbox: keep it in host-side state only (for
	// example resume state).
	PrivateKey string `json:"privateKey"`
	// PublicKey is the raw Ed25519 public key, base64url encoded. Safe to
	// pass into the sandbox (EnvAuthPublicKey).
	PublicKey string `json:"publicKey"`
}

// CreateBridgeAuthKeyPair returns a fresh Ed25519 key pair for one bridge
// session. Mirrors TS createBridgeAuthKeyPair.
func CreateBridgeAuthKeyPair() BridgeAuthKeyPair {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		// crypto/rand never fails on supported platforms.
		panic(fmt.Sprintf("harness bridge: crypto/rand: %v", err))
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		panic(fmt.Sprintf("harness bridge: marshal private key: %v", err))
	}
	return BridgeAuthKeyPair{
		PrivateKey: base64.RawURLEncoding.EncodeToString(der),
		PublicKey:  base64.RawURLEncoding.EncodeToString(pub),
	}
}

// ParseBridgeAuthPrivateKey decodes a BridgeAuthKeyPair.PrivateKey. Mirrors
// TS parseBridgeAuthPrivateKey, including its error texts; the key itself is
// never part of an error.
func ParseBridgeAuthPrivateKey(privateKey string) (ed25519.PrivateKey, error) {
	der, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(privateKey, "="))
	if err != nil || len(der) == 0 {
		return nil, errors.New("Invalid bridge authentication private key.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("Invalid bridge authentication private key.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("Bridge authentication requires an Ed25519 private key.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return edKey, nil
}

// ParseBridgeAuthPublicKey decodes a BridgeAuthKeyPair.PublicKey, as the
// bridge does at startup. Mirrors TS parseBridgeAuthPublicKey.
func ParseBridgeAuthPublicKey(publicKey string) (ed25519.PublicKey, error) {
	if !publicKeyRegexp.MatchString(publicKey) {
		return nil, errors.New("The bridge requires a valid Ed25519 public key for authentication.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	raw, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("The bridge requires a valid Ed25519 public key for authentication.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return ed25519.PublicKey(raw), nil
}

// bridgeAuthMessage is the domain-separated message signed for a challenge.
func bridgeAuthMessage(challenge string) []byte {
	return []byte(bridgeAuthMessagePrefix + challenge)
}

// NewBridgeAuthChallenge returns a fresh challenge (32 random bytes, hex),
// as the bridge sends for every connection.
func NewBridgeAuthChallenge() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("harness bridge: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// SerializeBridgeAuthChallengeFrame is the bridge's challenge frame.
func SerializeBridgeAuthChallengeFrame(challenge string) string {
	return `{"type":"bridge-auth-challenge","challenge":"` + challenge + `"}`
}

func parseBridgeAuthChallengeFrame(text string) (string, bool) {
	m := challengeFrameRegexp.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func serializeBridgeAuthProofFrame(signature string) string {
	return `{"type":"bridge-auth-proof","signature":"` + signature + `"}`
}

// CreateBridgeAuthProof answers a challenge frame with the proof frame: the
// challenge's domain-separated message signed with privateKey. It fails for
// anything but a canonical challenge frame. Mirrors TS createBridgeAuthProof.
func CreateBridgeAuthProof(privateKey ed25519.PrivateKey, message string) (string, error) {
	challenge, ok := parseBridgeAuthChallengeFrame(message)
	if !ok {
		return "", errors.New("Invalid bridge authentication challenge.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	signature := ed25519.Sign(privateKey, bridgeAuthMessage(challenge))
	return serializeBridgeAuthProofFrame(base64.RawURLEncoding.EncodeToString(signature)), nil
}

// VerifyBridgeAuthProof reports whether frame is a canonical proof frame
// carrying a valid signature of challenge by publicKey: the bridge side of
// the handshake (TS authenticateBridgeConnection). A binary frame, or one
// over AuthMaxFrameBytes, never verifies.
func VerifyBridgeAuthProof(publicKey ed25519.PublicKey, challenge string, frame []byte, binary bool) bool {
	if binary || len(frame) > AuthMaxFrameBytes || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	m := proofFrameRegexp.FindSubmatch(frame)
	if m == nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(string(m[1]))
	if err != nil {
		return false
	}
	return ed25519.Verify(publicKey, bridgeAuthMessage(challenge), signature)
}

// AuthenticationError means bridge authentication failed in a way retrying
// cannot fix: the bridge sent no valid challenge. (A bridge that rejects the
// proof closes with code 1008, reported as *UnauthorizedError.) Mirrors TS
// BridgeWebSocketAuthenticationError.
type AuthenticationError struct{ Message string }

func (e *AuthenticationError) Error() string { return e.Message }

const invalidChallengeMessage = "The harness bridge did not send a valid authentication challenge."

// BridgeAuthEnvironment returns the spawn environment of a public-key bridge:
// the authentication public key, the port and, for disk replay,
// BRIDGE_REPLAY_FROM_DISK=1. It carries no secret.
func BridgeAuthEnvironment(publicKey string, port int, replayFromDisk bool) map[string]string {
	env := map[string]string{EnvAuthPublicKey: publicKey, EnvWSPort: fmt.Sprint(port)}
	if replayFromDisk {
		env[EnvReplayFromDisk] = "1"
	}
	return env
}

// MintBridgeTokenIgnoredWarning is the warning for a passed mintBridgeToken
// setting (TS warnMintBridgeTokenIgnored).
func MintBridgeTokenIgnoredWarning(harnessID string) string {
	return "The " + harnessID + " harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair."
}

// WarnMintBridgeTokenIgnored warns, once per call, that a non-nil mint
// callback is ignored: bridge authentication uses an adapter-generated key
// pair instead of a shared token. warn receives the text (nil: log.Println,
// TS console.warn). Mirrors TS warnMintBridgeTokenIgnored.
func WarnMintBridgeTokenIgnored(harnessID string, mint harness.MintBridgeTokenCallback, warn func(string)) {
	if mint == nil {
		return
	}
	if warn == nil {
		warn = func(s string) { log.Println(s) }
	}
	warn(MintBridgeTokenIgnoredWarning(harnessID))
}
