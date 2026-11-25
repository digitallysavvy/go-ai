package revai

import (
	"testing"
	"time"
)

// TS fb3735938c catalog refactor (ruling 13): timeoutMs 60000 and
// pollingInterval 1000 came from getRevaiTranscriptionModelEntry for every
// model ID, known or not.
func TestPortProvcPollingDefaults(t *testing.T) {
	if pollTimeout != 60*time.Second || pollInterval != time.Second {
		t.Fatalf("pollTimeout = %v, pollInterval = %v", pollTimeout, pollInterval)
	}
}
