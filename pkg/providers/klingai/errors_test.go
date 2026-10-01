package klingai

import "testing"

func TestKlingAIErrorFormatting(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "with semantic code and details",
			err: &Error{
				ErrorCode: "KLINGAI_VIDEO_GENERATION_FAILED",
				Message:   "Video generation failed",
				Details:   "provider timeout",
			},
			want: "klingai error (KLINGAI_VIDEO_GENERATION_FAILED): Video generation failed - provider timeout",
		},
		{
			name: "with semantic code and no details",
			err: &Error{
				ErrorCode: ErrCodeKlingVideoMissingOptions,
				Message:   "VideoUrl is required for motion control",
			},
			want: "klingai error (KLINGAI_VIDEO_MISSING_OPTIONS): VideoUrl is required for motion control",
		},
		{
			name: "without semantic code but details",
			err: &Error{
				Code:    400,
				Message: "Invalid provider options",
				Details: "missing webhook URL",
			},
			want: "klingai error (code 400): Invalid provider options - missing webhook URL",
		},
		{
			name: "without semantic code and no details",
			err: &Error{
				Code:    401,
				Message: "Authentication failed",
			},
			want: "klingai error (code 401): Authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKlingAIErrorConstructors(t *testing.T) {
	plain := NewError(500, "x", "y")
	if plain.Code != 500 || plain.Message != "x" || plain.Details != "y" {
		t.Fatalf("NewError mismatch: %#v", plain)
	}

	auth := NewAuthError("bad token")
	if auth.Code != 401 || auth.Message != "Authentication failed" || auth.Details != "bad token" {
		t.Fatalf("NewAuthError mismatch: %#v", auth)
	}

	videoErr := NewVideoGenerationError("upstream failed")
	if videoErr.ErrorCode != "KLINGAI_VIDEO_GENERATION_ERROR" || videoErr.Code != 500 {
		t.Fatalf("NewVideoGenerationError mismatch: %#v", videoErr)
	}

	timeout := NewTimeoutError("45s")
	if timeout.Code != 504 || timeout.Details != "Generation timed out after 45s" {
		t.Fatalf("NewTimeoutError mismatch: %#v", timeout)
	}

	invalid := NewInvalidOptionsError("missing mode")
	if invalid.Code != 400 || invalid.Message != "Invalid provider options" {
		t.Fatalf("NewInvalidOptionsError mismatch: %#v", invalid)
	}

	failed := NewVideoGenerationFailedError("task failed")
	if failed.ErrorCode != "KLINGAI_VIDEO_GENERATION_FAILED" || failed.Code != 500 {
		t.Fatalf("NewVideoGenerationFailedError mismatch: %#v", failed)
	}

	missing := NewMissingVideoOptionsError("Mode")
	if missing.ErrorCode != ErrCodeKlingVideoMissingOptions || missing.Code != 400 {
		t.Fatalf("NewMissingVideoOptionsError mismatch: %#v", missing)
	}
}
