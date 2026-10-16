package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TS maps response.container.skills with `?.map(...) ?? null`, so an empty
// skills array stays [] while an absent or null one is null.
func TestContainerSkillsMetadataEmptyListStaysEmpty(t *testing.T) {
	cases := map[string]string{
		`{"id":"c","expires_at":"e","skills":[]}`:   `[]`,
		`{"id":"c","expires_at":"e","skills":null}`: `null`,
		`{"id":"c","expires_at":"e"}`:               `null`,
	}
	for wire, want := range cases {
		var c anthropicContainerResponse
		if err := json.Unmarshal([]byte(wire), &c); err != nil {
			t.Fatal(err)
		}
		meta := anthropicProviderMetadata(anthropicUsage{}, "", nil, &c, nil)
		container := meta["anthropic"].(map[string]interface{})["container"].(map[string]interface{})
		got, _ := json.Marshal(container["skills"])
		if string(got) != want {
			t.Errorf("%s: skills = %s, want %s", wire, got, want)
		}
	}
}

// TS builds the message_start container as {expiresAt, id, skills: null}.
func TestStreamMessageStartContainerSkillsAreNull(t *testing.T) {
	sse := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"container\":{\"id\":\"c1\",\"expires_at\":\"e\",\"skills\":[{\"type\":\"anthropic\",\"skill_id\":\"pptx\",\"version\":\"1\"}]},\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	if stream.container == nil || stream.container.Skills != nil {
		t.Fatalf("container = %+v", stream.container)
	}
}
