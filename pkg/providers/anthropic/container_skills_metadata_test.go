package anthropic

import (
	"encoding/json"
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
