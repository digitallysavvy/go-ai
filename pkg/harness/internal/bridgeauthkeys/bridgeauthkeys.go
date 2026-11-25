// Package bridgeauthkeys creates the per-session bridge authentication key
// pair of the bridge-backed harness adapters (TS c849d37dac
// createBridgeAuthKeyPair). It exists so the adapters' tests, which run an
// in-process fake bridge, can know the public key a session will use. The
// package is internal: code outside pkg/harness cannot fix the key.
package bridgeauthkeys

import (
	"sync"
	"sync/atomic"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

var (
	fixed       atomic.Pointer[bridge.BridgeAuthKeyPair]
	testingOnce sync.Once
	testingKey  bridge.BridgeAuthKeyPair
)

// New returns a fresh key pair (bridge.CreateBridgeAuthKeyPair), or the key
// pair a test fixed with Testing or SetForTesting.
func New() bridge.BridgeAuthKeyPair {
	if kp := fixed.Load(); kp != nil {
		return *kp
	}
	return bridge.CreateBridgeAuthKeyPair()
}

// Testing fixes New, for the rest of the test binary, to one key pair and
// returns it, so a fake bridge can verify the sessions' proofs. For tests
// only.
func Testing() bridge.BridgeAuthKeyPair {
	testingOnce.Do(func() {
		testingKey = bridge.CreateBridgeAuthKeyPair()
		fixed.Store(&testingKey)
	})
	return testingKey
}

// SetForTesting makes New return *kp (a fresh pair per call when kp is nil)
// until the returned restore function runs. For tests only.
func SetForTesting(kp *bridge.BridgeAuthKeyPair) (restore func()) {
	prev := fixed.Swap(kp)
	return func() { fixed.Store(prev) }
}
