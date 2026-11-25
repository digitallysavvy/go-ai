package bridge

import "testing"

// harnessV1BridgeReadySchema through safeParseJSON: zod matches keys
// exactly, so a differently cased "Port" key is stripped (node, zod 4.6.7:
// both lines parse with port 1). Coverage only: securejson.Unmarshal
// already matches keys exactly.
func TestR99carryDecodeReadyExactKeys(t *testing.T) {
	for _, line := range []string{
		`{"type":"bridge-ready","port":1,"Port":2}`,
		`{"type":"bridge-ready","port":1,"PORT":"x"}`,
	} {
		r, err := DecodeReady([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if r.Port != 1 {
			t.Fatalf("%s: port = %d, want 1", line, r.Port)
		}
	}
	if _, err := DecodeReady([]byte(`{"type":"bridge-ready","port":1,"__proto__":{}}`)); err == nil {
		t.Fatal("__proto__: want the secureJsonParse rejection")
	}
}

// The adapters' inbound union (harnessV1BridgeStartBaseSchema plus the shared
// commands), checked with node and zod 4.6.7: every frame here fails
// safeParse except the last two, and a text responseFormat strips its other
// keys whatever their values.
func TestR99carryDecodeInboundMatchesZod(t *testing.T) {
	for _, frame := range []string{
		`{"type":"tool-result","toolCallId":"c","output":1,"isError":null}`,
		`{"type":"tool-approval-response","approvalId":"a","approved":true,"reason":null}`,
		`{"type":"user-message","text":"t","messageId":null}`,
		`{"type":"start","prompt":"p","model":null}`,
		`{"type":"start","prompt":"p","permissionMode":"nope"}`,
		`{"type":"start","prompt":"p","permissionMode":""}`,
		`{"type":"start","prompt":"p","tools":[{"description":"d"}]}`,
		`{"type":"start","prompt":"p","tools":[{"name":"n","description":null}]}`,
		`{"type":"start","prompt":"p","debug":{"level":"loud"}}`,
		`{"type":"start","prompt":"p","debug":{"level":null}}`,
		`{"type":"start","prompt":"p","debug":{"subsystems":[null]}}`,
		`{"type":"start","prompt":"p","builtinToolFiltering":{"mode":"allow"}}`,
		`{"type":"start","prompt":"p","builtinToolFiltering":{"mode":"maybe","toolNames":[]}}`,
		`{"type":"start","prompt":"p","builtinToolFiltering":{"mode":"deny","toolNames":[null]}}`,
		`{"type":"start","prompt":"p","responseFormat":{"type":"json","name":null}}`,
		`{"type":"start","prompt":"p","responseFormat":{"type":"json","schema":{"a":1e400}}}`,
		`{"type":"start","prompt":"p","responseFormat":{"type":"xml"}}`,
		`{"type":"start","prompt":"p","responseFormat":{}}`,
	} {
		if cmd, err := DecodeInbound([]byte(frame)); err == nil {
			t.Errorf("%s: decoded %+v, want an error", frame, cmd)
		}
	}

	if _, err := DecodeInbound([]byte(`{"type":"tool-result","toolCallId":"c","output":1,"toolResult":null}`)); err != nil {
		t.Errorf("toolResult null (z.unknown): %v", err)
	}
	cmd, err := DecodeInbound([]byte(`{"type":"start","prompt":"p","responseFormat":{"type":"text","name":5,"schema":"x"}}`))
	if err != nil {
		t.Fatalf("text responseFormat with stripped keys: %v", err)
	}
	rf := cmd.(*StartBase).ResponseFormat
	if rf == nil || rf.Type != "text" || rf.Name != "" || rf.Schema != nil {
		t.Fatalf("responseFormat = %+v, want {type: text}", rf)
	}
	full := `{"type":"start","prompt":"p","tools":[{"name":"n"}],"permissionMode":"allow-all","debug":{"level":"info","subsystems":["a"]},"builtinToolFiltering":{"mode":"deny","toolNames":["x"]},"responseFormat":{"type":"json","schema":{"a":1},"name":"n"}}`
	cmd, err = DecodeInbound([]byte(full))
	if err != nil {
		t.Fatalf("full start: %v", err)
	}
	if b := cmd.(*StartBase); b.ResponseFormat.Name != "n" || b.Debug.Level != "info" || len(b.Tools) != 1 {
		t.Fatalf("full start = %+v", b)
	}
}
