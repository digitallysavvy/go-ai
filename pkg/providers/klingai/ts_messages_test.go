package klingai

import "testing"

// JSON.stringify of a zod-parsed value: schema key order, unknown keys gone.
func TestZodStringifyMatchesParsedValue(t *testing.T) {
	got := zodStringify([]byte(`{"request_id":"r","extra":1,"message":"success","code":0,"data":null}`), klingaiCreateSpec)
	if want := `{"code":0,"message":"success","request_id":"r","data":null}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	got = zodStringify([]byte(`{"code":0,"message":"ok","data":{"task_status":"succeed","task_id":"t","junk":1,"task_result":{"videos":[{"url":"u","id":"i","x":2}]}}}`), klingaiStatusSpec)
	if want := `{"code":0,"message":"ok","data":{"task_id":"t","task_status":"succeed","task_result":{"videos":[{"id":"i","url":"u"}]}}}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
