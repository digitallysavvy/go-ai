package fal

import "testing"

func TestFalOptionsAreValidated(t *testing.T) {
	check := func(name string, validate func(map[string]interface{}) error, bad, good []map[string]interface{}) {
		for i, o := range bad {
			if validate(map[string]interface{}{"fal": o}) == nil {
				t.Fatalf("%s bad case %d (%v) accepted", name, i, o)
			}
		}
		for i, o := range good {
			if err := validate(map[string]interface{}{"fal": o}); err != nil {
				t.Fatalf("%s good case %d (%v) rejected: %v", name, i, o, err)
			}
		}
	}
	check("image", validateImageOptions,
		[]map[string]interface{}{
			{"guidanceScale": 0}, {"guidanceScale": 21}, {"numInferenceSteps": 51}, {"outputFormat": "webp"}, {"acceleration": "max"},
			{"safetyTolerance": "7"}, {"safetyTolerance": 7}, {"useMultipleImages": "yes"}, {"imageUrl": 1}, {"sync_mode": 1},
		},
		[]map[string]interface{}{
			{"guidanceScale": 1, "numInferenceSteps": 50, "outputFormat": "png", "safetyTolerance": "3", "model_key": true},
			{"safety_tolerance": 6, "guidance_scale": nil, "useMultipleImages": true},
		})
	check("speech", validateSpeechOptions,
		[]map[string]interface{}{
			{"voice_setting": map[string]interface{}{"emotion": "bored"}}, {"voice_setting": map[string]interface{}{"speed": "fast"}},
			{"language_boost": "Klingon"}, {"audio_setting": "x"}, {"pronunciation_dict": map[string]interface{}{"a": 1}},
		},
		[]map[string]interface{}{
			{"voice_setting": map[string]interface{}{"emotion": "happy", "speed": 1.1}, "language_boost": "Chinese,Yue", "pronunciation_dict": map[string]interface{}{"a": "b"}, "audio_setting": map[string]interface{}{"any": 1}},
		})
	check("transcription", validateTranscriptionOptions,
		[]map[string]interface{}{{"language": 1}, {"diarize": "yes"}, {"chunkLevel": "line"}, {"version": "4"}, {"batchSize": "64"}},
		[]map[string]interface{}{{"language": "fr", "diarize": false, "chunkLevel": "word", "version": "3", "batchSize": 8, "numSpeakers": nil}})
	check("video", validateVideoOptions,
		[]map[string]interface{}{{"loop": "yes"}, {"motionStrength": 2}, {"resolution": 720}, {"promptOptimizer": 1}},
		[]map[string]interface{}{{"loop": true, "motionStrength": 0.5, "resolution": "720p", "custom": 1}})
}

func TestFalVideoBodiesAreValidated(t *testing.T) {
	for _, body := range []string{`{"request_id":1}`, `<html>`} {
		if validateVideoJob(200, nil, []byte(body)) == nil {
			t.Fatalf("job body %q accepted", body)
		}
	}
	for _, body := range []string{`{"video":{"width":1}}`, `{"video":{"url":"u"},"has_nsfw_concepts":["no"]}`, `{"seed":"1"}`} {
		if validateVideoStatus(200, nil, []byte(body)) == nil {
			t.Fatalf("status body %q accepted", body)
		}
	}
	if err := validateVideoStatus(200, nil, []byte(`{"video":{"url":"u","width":null},"has_nsfw_concepts":[false],"timings":{"inference":1.5},"prompt":null,"extra":1}`)); err != nil {
		t.Fatalf("valid status body rejected: %v", err)
	}
}
