package codex

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port2toolsChannelKeepAlive is the heartbeat interval of a session's bridge
// channel.
func Port2toolsChannelKeepAlive(s harness.Session) time.Duration {
	return s.(*session).opts.channel.KeepAlive()
}
