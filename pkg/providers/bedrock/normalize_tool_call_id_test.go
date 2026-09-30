package bedrock

import "testing"

// TestIsMistralModel ports TS normalize-tool-call-id.test.ts "isMistralModel".
func TestIsMistralModel(t *testing.T) {
	trueCases := []string{
		"mistral.mistral-7b-instruct-v0:2",
		"mistral.mixtral-8x7b-instruct-v0:1",
		"mistral.mistral-large-2402-v1:0",
		"mistral.mistral-small-2402-v1:0",
		"mistral.mistral-large-2407-v1:0",
		"mistral.ministral-3-14b-instruct",
		"mistral.ministral-3-8b-instruct",
		"us.mistral.pixtral-large-2502-v1:0",
		"eu.mistral.mistral-large-2407-v1:0",
	}
	for _, id := range trueCases {
		if !isMistralModel(id) {
			t.Errorf("isMistralModel(%q) = false, want true", id)
		}
	}

	falseCases := []string{
		"anthropic.claude-3-5-sonnet-20241022-v2:0",
		"amazon.nova-pro-v1:0",
		"openai.gpt-4o",
		"meta.llama3-70b-instruct-v1:0",
	}
	for _, id := range falseCases {
		if isMistralModel(id) {
			t.Errorf("isMistralModel(%q) = true, want false", id)
		}
	}
}

// TestNormalizeToolCallID ports TS normalize-tool-call-id.test.ts
// "normalizeToolCallId". The hashed-ID expectations are the exact TS
// vitest inline-snapshot values, used here as cross-implementation test
// vectors to verify the Go FNV-1a -> base62 port matches TS bit-for-bit.
func TestNormalizeToolCallID(t *testing.T) {
	t.Run("returns the original ID when not a Mistral model", func(t *testing.T) {
		const originalID = "tooluse_bpe71yCfRu2b5i-nKGDr5g"
		if got := normalizeToolCallID(originalID, false); got != originalID {
			t.Errorf("normalizeToolCallID = %q, want %q", got, originalID)
		}
	})

	t.Run("hashes incompatible IDs deterministically for Mistral models", func(t *testing.T) {
		const toolCallID = "tooluse_bpe71yCfRu2b5i-nKGDr5g"
		const want = "8eHypBDcw"
		if got := normalizeToolCallID(toolCallID, true); got != want {
			t.Errorf("normalizeToolCallID = %q, want %q", got, want)
		}
		if got := normalizeToolCallID(toolCallID, true); got != want {
			t.Errorf("normalizeToolCallID (2nd call) = %q, want %q", got, want)
		}
	})

	t.Run("produces 9 alphanumeric characters for incompatible IDs", func(t *testing.T) {
		cases := []struct {
			input string
			want  string
		}{
			{"tool-use_123ABC456", "hvVDqPNyj"},
			{"___abc123DEF___", "TnzPqldGU"},
			{"abc", "GRuIyUwcV"},
			{"12345", "PceAgWDYe"},
			{"___---___", "5C589HVqG"},
		}
		for _, tt := range cases {
			got := normalizeToolCallID(tt.input, true)
			if got != tt.want {
				t.Errorf("normalizeToolCallID(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if !validMistralToolCallIDPattern.MatchString(got) {
				t.Errorf("normalizeToolCallID(%q) = %q, does not match ^[a-zA-Z0-9]{9}$", tt.input, got)
			}
		}
	})

	t.Run("preserves IDs that are already valid Mistral tool call IDs", func(t *testing.T) {
		if got := normalizeToolCallID("abcdefghi", true); got != "abcdefghi" {
			t.Errorf("normalizeToolCallID = %q, want abcdefghi", got)
		}
		if got := normalizeToolCallID("abc123XYZ", true); got != "abc123XYZ" {
			t.Errorf("normalizeToolCallID = %q, want abc123XYZ", got)
		}
	})

	t.Run("keeps distinct Bedrock IDs distinct when their prefixes match", func(t *testing.T) {
		got1 := normalizeToolCallID("tooluse_Ac1Xq9ZklmNoPq", true)
		got2 := normalizeToolCallID("tooluse_Ac2Yt7WrstUvWx", true)
		if got1 != "7rDVWRig0" {
			t.Errorf("normalizeToolCallID(1) = %q, want 7rDVWRig0", got1)
		}
		if got2 != "bNZvZKNBZ" {
			t.Errorf("normalizeToolCallID(2) = %q, want bNZvZKNBZ", got2)
		}
		if got1 == got2 {
			t.Errorf("expected distinct hashes, got %q for both", got1)
		}
	})
}
