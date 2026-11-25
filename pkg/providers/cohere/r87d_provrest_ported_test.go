package cohere

import (
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// Hand port of the cohere TS assertion the replay harness does not evaluate
// (R87D_TSFIXTURE closure rule): `toBe(undefined)` of RunFn's result, which
// the JS fallback does not get (the call is the test's own function call).

var r87dprovrestPorted = map[string]string{
	tsfixture.PortedKey("should handle auto tool choice", "result.toolChoice", "toBe"): "TestR87dprovrestCoherePrepareToolsAutoChoice",
}

func init() { tsfixture.RegisterPorted(r87dprovrestPorted) }

// TestMain fails an unfiltered run whose hand-port marks are stale.
func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m, r87dprovrestPorted)) }

// cohere-prepare-tools.test.ts 78-85: expect(result.toolChoice).toBe(undefined)
func TestR87dprovrestCoherePrepareToolsAutoChoice(t *testing.T) {
	const test = "should handle auto tool choice"
	var args []interface{}
	for _, c := range tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz") {
		if c.Test == test {
			args = c.FnArgs
		}
	}
	in, _ := args[0].(map[string]interface{})
	ts, why := tsfixture.BuildTools(in["tools"])
	if why != "" {
		t.Fatal(why)
	}
	tc, _ := in["toolChoice"].(map[string]interface{})
	if tc["type"] != "auto" {
		t.Fatalf("case input toolChoice = %v", tc)
	}
	tools, toolChoice, _ := prepareCohereTools(ts, tsfixture.BuildToolChoice(tc), true)
	if len(tools) != 1 {
		t.Errorf("tools = %v", tools)
	}
	if toolChoice != nil {
		t.Errorf("result.toolChoice = %#v, TS undefined", toolChoice)
	}
}
