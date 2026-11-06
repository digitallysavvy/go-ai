package google

import "sync/atomic"

// testHideAudioEnded is a test-only seam: while it is true the live
// streams leave the audio-ended channel out of that select iteration, which
// forces a ready clean close to win the select deterministically.
var testHideAudioEnded atomic.Bool
