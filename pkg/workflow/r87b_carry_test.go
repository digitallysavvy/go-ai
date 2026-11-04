package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// V061 item 44 class sweep: TS serializeToolSet (workflow/src/
// serializable-schema.ts at 3fab50aa9b) keeps `inputExamples` when it is
// `!= null`, so an empty array crosses the durable boundary as [] and the
// resolved tool still has it (prepareTools then sends inputExamples: []).
// A nil slice stays absent.
func TestR87bcarrySerializableToolDefEmptyInputExamples(t *testing.T) {
	t.Parallel()
	defs, err := SerializeToolSet([]types.Tool{
		{Name: "empty", InputExamples: []types.ToolInputExample{}},
		{Name: "unset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyJSON, err := json.Marshal(defs["empty"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(emptyJSON), `"inputExamples":[]`) {
		t.Fatalf("empty examples not kept: %s", emptyJSON)
	}
	unsetJSON, err := json.Marshal(defs["unset"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unsetJSON), "inputExamples") {
		t.Fatalf("nil examples written: %s", unsetJSON)
	}
	var decoded SerializableToolDef
	if err := json.Unmarshal(emptyJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	tools := ResolveSerializableTools(map[string]SerializableToolDef{"empty": decoded})
	if len(tools) != 1 || tools[0].InputExamples == nil || len(tools[0].InputExamples) != 0 {
		t.Fatalf("resolved InputExamples = %#v, want a non-nil empty slice", tools)
	}
}
