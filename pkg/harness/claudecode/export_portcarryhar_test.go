package claudecode

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// PortcarryharChannelKeepAlive is the heartbeat interval of a session's
// bridge channel.
func PortcarryharChannelKeepAlive(s harness.Session) time.Duration {
	return s.(*session).opts.channel.KeepAlive()
}
