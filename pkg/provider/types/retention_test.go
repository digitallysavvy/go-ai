package types

import "testing"

func TestRetentionSettingsDefaults(t *testing.T) {
	var nilSettings *RetentionSettings
	if !nilSettings.ShouldRetainRequestBody() || !nilSettings.ShouldRetainResponseBody() {
		t.Fatal("nil retention settings should default to retaining request/response")
	}

	s := &RetentionSettings{}
	if !s.ShouldRetainRequestBody() || !s.ShouldRetainResponseBody() {
		t.Fatal("unset retention fields should default to retaining request/response")
	}
}

func TestRetentionSettingsExplicitValues(t *testing.T) {
	s := &RetentionSettings{
		RequestBody:  BoolPtr(false),
		ResponseBody: BoolPtr(true),
	}
	if s.ShouldRetainRequestBody() {
		t.Fatal("request body should not be retained when explicitly false")
	}
	if !s.ShouldRetainResponseBody() {
		t.Fatal("response body should be retained when explicitly true")
	}

	if p := BoolPtr(true); p == nil || *p != true {
		t.Fatalf("BoolPtr(true) returned invalid pointer: %+v", p)
	}
}
