package google

import (
	"net"
	"sync/atomic"
)

// testHideAudioEnded is a test-only seam: while it is true the live
// streams leave the audio-ended channel out of that select iteration, which
// forces a ready clean close to win the select deterministically.
var testHideAudioEnded atomic.Bool

// testWrapConn is a test-only seam: when set, the live streams wrap the raw
// network connection with it (after the close-frame sniffer), so a test can
// fail a socket write.
var testWrapConn func(net.Conn) net.Conn

// liveWrapConn composes the close-frame sniffer with testWrapConn.
func liveWrapConn(wrap func(net.Conn) net.Conn) func(net.Conn) net.Conn {
	if testWrapConn == nil {
		return wrap
	}
	return func(c net.Conn) net.Conn { return testWrapConn(wrap(c)) }
}
