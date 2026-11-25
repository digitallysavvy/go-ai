package claudecode

import "github.com/digitallysavvy/go-ai/pkg/harness/bridge"

// R91carryTestKeys is the bridge authentication key pair every spawn uses in
// this package's tests, so the fake bridges (bridgetest.Options.PublicKey)
// can verify the host's proof.
var R91carryTestKeys = bridge.CreateBridgeAuthKeyPair()

func init() {
	createBridgeAuthKeyPair = func() bridge.BridgeAuthKeyPair { return R91carryTestKeys }
}
