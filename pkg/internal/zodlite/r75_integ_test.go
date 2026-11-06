package zodlite

import (
	"strings"
	"testing"
)

// MaxItems' too_big is a check issue (zod `continue: true`): in a union
// whose other member aborts, zod v4 handleUnionResults reports the single
// non-aborted member's issues directly instead of an invalid_union (review
// round 75, provb fix 1).
func TestR75Integ_MaxItemsContinueInUnion(t *testing.T) {
	n := Union(MaxItems(Array(String), 1), Number)
	got := IssueText(ValidationMessage(n, `["a","b"]`))
	if strings.Contains(got, "invalid_union") || !strings.Contains(got, `"too_big"`) {
		t.Fatalf("issues = %s, want the array member's too_big only", got)
	}
}
