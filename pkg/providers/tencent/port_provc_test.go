package tencent

import "testing"

// TS 790b2171b0 catalog refactor (ruling 13): the TokenHub Kling audio /
// multi-shot and omni predicates moved into getTencentVideoModelEntry. An
// unknown kling-video-* ID keeps both audio and multi-shot and is not omni
// unless its name ends in -omni (which sets kind, not the policy).
func TestPortProvcKlingPoliciesOnUnknownIDs(t *testing.T) {
	for id, wantAudio := range map[string]bool{
		"kling-video-v4": false, "kling-video-v3-turbo": false,
		"kling-video-v3": true, "kling-video-v5": true, "kling-video-future-omni": true, "other": true,
	} {
		if got := !klingModelsWithoutAudioOrMultiShot[id]; got != wantAudio {
			t.Errorf("audio and multi-shot for %q = %v, want %v", id, got, wantAudio)
		}
	}
	for id, want := range map[string]bool{"kling-video-v4": true, "kling-video-v3-turbo": false, "kling-video-v5": false, "kling-video-v5-omni": false} {
		if got := klingStandardModelsWithOmni[id]; got != want {
			t.Errorf("standard model with omni %q = %v, want %v", id, got, want)
		}
	}
}
