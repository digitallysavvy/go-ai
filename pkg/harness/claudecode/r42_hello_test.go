package claudecode

import (
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// TS onHello assigns the capability of every hello, so a later hello
// without it turns user-message responses off again.
func TestR42TrackUserMessageResponsesFollowsEachHello(t *testing.T) {
	var supported atomic.Bool
	track := trackUserMessageResponses(&supported)
	yes := true
	track(&bridge.Hello{Capabilities: &bridge.HelloCapabilities{ExperimentalUserMessageResponses: &yes}})
	if !supported.Load() {
		t.Fatal("want supported after a hello with the capability")
	}
	track(&bridge.Hello{})
	if supported.Load() {
		t.Fatal("want unsupported after a hello without the capability")
	}
}
