package xai

import "testing"

func TestModerationErrorFormatting(t *testing.T) {
	withCode := (&ModerationError{Code: "blocked", Message: "content policy"}).Error()
	if withCode != "xai: moderation rejection [blocked]: content policy" {
		t.Fatalf("with code Error() = %q", withCode)
	}

	withoutCode := (&ModerationError{Message: "content policy"}).Error()
	if withoutCode != "xai: moderation rejection: content policy" {
		t.Fatalf("without code Error() = %q", withoutCode)
	}
}

func TestXAIStreamErrorFormatting(t *testing.T) {
	withCode := (&XAIStreamError{Code: "rate_limit", Message: "too many requests"}).Error()
	if withCode != "xai.responses stream error [rate_limit]: too many requests" {
		t.Fatalf("with code Error() = %q", withCode)
	}

	withoutCode := (&XAIStreamError{Message: "too many requests"}).Error()
	if withoutCode != "xai.responses stream error: too many requests" {
		t.Fatalf("without code Error() = %q", withoutCode)
	}
}

func TestXAIStreamIncompleteFormatting(t *testing.T) {
	withReason := (&XAIStreamIncomplete{Reason: "max_output_tokens"}).Error()
	if withReason != "xai.responses stream incomplete: max_output_tokens" {
		t.Fatalf("with reason Error() = %q", withReason)
	}

	withoutReason := (&XAIStreamIncomplete{}).Error()
	if withoutReason != "xai.responses stream incomplete" {
		t.Fatalf("without reason Error() = %q", withoutReason)
	}
}

func TestXAIStreamFailedFormatting(t *testing.T) {
	tests := []struct {
		name string
		err  *XAIStreamFailed
		want string
	}{
		{
			name: "message has precedence",
			err:  &XAIStreamFailed{Reason: "timeout", Code: "E1", Message: "provider failed"},
			want: "xai.responses stream failed: provider failed",
		},
		{
			name: "reason used when message empty",
			err:  &XAIStreamFailed{Reason: "timeout", Code: "E1"},
			want: "xai.responses stream failed: timeout",
		},
		{
			name: "code used when message and reason empty",
			err:  &XAIStreamFailed{Code: "E1"},
			want: "xai.responses stream failed: E1",
		},
		{
			name: "fallback default",
			err:  &XAIStreamFailed{},
			want: "xai.responses stream failed",
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
