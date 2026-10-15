package langchain

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Replays adapter.test.ts "toUIMessageStream with LangGraph HITL fixture" and
// "end-to-end with processUIMessageStream": the real LangGraph streams in
// __fixtures__/langgraph.ts must give the committed TS snapshots
// (__snapshots__/*.json), with Date.now() pinned to 1234567890. The cases are
// committed gzipped in testdata/tscases (extracted from the TS SDK at
// b658e8503c; fixtures via a vm evaluation of langgraph.ts).
func readGz(t *testing.T, path string, v interface{}) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	if err := json.NewDecoder(zr).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func replayFixture(t *testing.T, events []interface{}) []interface{} {
	t.Helper()
	old := nowMillis
	nowMillis = func() int64 { return 1234567890 }
	defer func() { nowMillis = old }()

	ch := make(chan []interface{}, len(events))
	for _, e := range events {
		ch <- e.([]interface{})
	}
	close(ch)
	out, errs := ToUIMessageStreamFromRaw(context.Background(), ch)
	chunks := collectOutputChunks(t, out, errs)
	// Normalize through JSON like JSON.stringify(result).
	data, err := json.Marshal(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var got []interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTSFixtureLangGraphSnapshots(t *testing.T) {
	var fixtures map[string][]interface{}
	readGz(t, "testdata/tscases/langgraph_fixtures.json.gz", &fixtures)
	cases := []struct{ fixture, snapshot string }{
		{"LANGGRAPH_RESPONSE_1", "langgraph-hitl-request-1"},
		{"LANGGRAPH_RESPONSE_2", "langgraph-hitl-request-2"},
		{"REACT_AGENT_TOOL_CALLING", "react-agent-tool-calling"},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			var want []interface{}
			readGz(t, "testdata/tscases/"+c.snapshot+".json.gz", &want)
			got := replayFixture(t, fixtures[c.fixture])
			if len(got) != len(want) {
				t.Logf("got %d chunks, want %d", len(got), len(want))
			}
			for i := 0; i < len(got) && i < len(want); i++ {
				if !reflect.DeepEqual(got[i], want[i]) {
					gj, _ := json.Marshal(got[i])
					wj, _ := json.Marshal(want[i])
					t.Fatalf("chunk %d differs\n got: %s\nwant: %s", i, gj, wj)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("chunk count %d, want %d", len(got), len(want))
			}
		})
	}
}

// Every reasoning-delta must follow a reasoning-start with the same id that is
// not yet closed (adapter.test.ts assertAdapterOutputIsValidForClient).
func TestTSFixtureReasoningLifecycleValidForClient(t *testing.T) {
	var fixtures map[string][]interface{}
	readGz(t, "testdata/tscases/langgraph_fixtures.json.gz", &fixtures)
	for name, events := range fixtures {
		t.Run(name, func(t *testing.T) {
			active := map[string]bool{}
			for _, c := range replayFixture(t, events) {
				chunk := c.(map[string]interface{})
				id, _ := chunk["id"].(string)
				switch chunk["type"] {
				case "reasoning-start":
					active[id] = true
				case "reasoning-delta":
					if !active[id] {
						t.Fatalf("reasoning-delta %q without open reasoning-start", id)
					}
				case "reasoning-end":
					delete(active, id)
				case "finish-step":
					active = map[string]bool{}
				}
			}
		})
	}
}

// Replays the inline-snapshot and toEqual cases of adapter.test.ts for
// toUIMessageStream. Each case is the TS test's input stream (class instances
// serialized the way RemoteGraph sends them) with the chunks that satisfy the
// TS assertions. Cases that depend on class-instance behavior (an AIMessage
// instance being ignored, messageConcat) or only on client assembly are not
// included. Extracted from b658e8503c.
func TestTSAdapterCases(t *testing.T) {
	var cases []struct {
		Name   string        `json:"name"`
		Events []interface{} `json:"events"`
		Opts   struct {
			SendStart  *bool `json:"sendStart"`
			SendFinish *bool `json:"sendFinish"`
		} `json:"opts"`
		Expected []interface{} `json:"expected"`
	}
	readGz(t, "testdata/tscases/adapter_cases.json.gz", &cases)
	if len(cases) < 50 {
		t.Fatalf("only %d cases loaded", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ch := make(chan interface{}, len(c.Events))
			for _, e := range c.Events {
				ch <- e
			}
			close(ch)
			out, errs := ToUIMessageStreamFromAnyWithCallbacks(context.Background(), ch,
				&StreamCallbacks{SendStart: c.Opts.SendStart, SendFinish: c.Opts.SendFinish})
			chunks := collectOutputChunks(t, out, errs)
			data, err := json.Marshal(chunks)
			if err != nil {
				t.Fatal(err)
			}
			var got []interface{}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got == nil {
				got = []interface{}{}
			}
			want := c.Expected
			if want == nil {
				want = []interface{}{}
			}
			if !reflect.DeepEqual(got, want) {
				gj, _ := json.Marshal(got)
				wj, _ := json.Marshal(want)
				t.Fatalf("chunks differ\n got: %s\nwant: %s", gj, wj)
			}
		})
	}
}
