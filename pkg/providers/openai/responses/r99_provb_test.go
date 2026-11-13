package responses

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR99provbUsagePlatformInt: a fractional-path Responses usage count
// beyond the platform int is clamped, not an implementation-defined
// conversion (MinInt64 on amd64, wrapped on 32-bit targets).
func TestR99provbUsagePlatformInt(t *testing.T) {
	var u ResponsesAPIUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":1e19,"output_tokens":2.5,"input_tokens_details":{"cached_tokens":1e19}}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != math.MaxInt || u.OutputTokens != 2 || u.InputTokensDetails == nil || u.InputTokensDetails.CachedTokens != math.MaxInt {
		t.Errorf("usage = %+v", u)
	}
}

// TestR99provbPartMetadataOutOfRange: TS holds part provider metadata as an
// object (a 1e400 value reads as Infinity, ruling 4). Go decoded it with
// encoding/json, which fails on 1e400 and dropped the metadata.
func TestR99provbPartMetadataOutOfRange(t *testing.T) {
	meta := json.RawMessage(`{"xai":{"itemId":"i1","n":1e400}}`)
	if v, ok := xaiPartString(nil, meta, "itemId"); !ok || v != "i1" {
		t.Errorf("xaiPartString = %q, %v, want i1", v, ok)
	}
	if got := toolCallContentProviderMetadata(types.ToolCallContent{ProviderMetadata: meta}); got["xai"] == nil {
		t.Errorf("tool call metadata = %v, want the xai namespace", got)
	}
	if got := toolResultContentProviderMetadata(types.ToolResultContent{ProviderMetadata: meta}); got["xai"] == nil {
		t.Errorf("tool result metadata = %v, want the xai namespace", got)
	}
}

// TestR99provbFunctionToolNonFiniteSchema: JSON.stringify writes a
// non-finite number as null (ruling 4). jsonstr could not null one reached
// through the unexported embedded alias, so a tool with empty
// allowedCallers and such a schema failed to marshal.
func TestR99provbFunctionToolNonFiniteSchema(t *testing.T) {
	d := FunctionToolDef{Type: "function", Name: "f", Parameters: map[string]interface{}{"maximum": math.Inf(1)}, AllowedCallers: []string{}}
	b, err := json.Marshal(d)
	if err != nil || !strings.Contains(string(b), `"maximum":null`) || !strings.Contains(string(b), `"allowed_callers":[]`) {
		t.Errorf("marshal = %s, %v", b, err)
	}
}
