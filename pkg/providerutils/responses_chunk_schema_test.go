package providerutils

import "testing"

func TestValidateResponsesChunk(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		bad  bool
	}{
		{"text delta", `{"type":"response.output_text.delta","item_id":"m","delta":"a"}`, false},
		{"text delta logprobs", `{"type":"response.output_text.delta","item_id":"m","delta":"a","logprobs":[{"token":"a","logprob":0,"top_logprobs":[]}]}`, false},
		{"text delta no item_id", `{"type":"response.output_text.delta","delta":"a"}`, true},
		{"text delta number", `{"type":"response.output_text.delta","item_id":"m","delta":1}`, true},
		{"created", `{"type":"response.created","response":{"id":"r","created_at":1.5,"model":"m"}}`, false},
		{"created no created_at", `{"type":"response.created","response":{"id":"r","model":"m"}}`, true},
		{"completed", `{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":null}}}}`, false},
		{"completed usage string", `{"type":"response.completed","response":{"usage":{"input_tokens":"1","output_tokens":2}}}`, true},
		{"failed needs sequence", `{"type":"response.failed","response":{}}`, true},
		{"unmodeled type passes", `{"type":"response.queued","anything":5}`, false},
		{"no type", `{"foo":1}`, true},
		{"type number", `{"type":5}`, true},
		{"item unknown type passes", `{"type":"response.output_item.added","output_index":0,"item":{"type":"brand_new"}}`, false},
		{"item no type fails", `{"type":"response.output_item.added","output_index":0,"item":{}}`, true},
		{"item not object fails", `{"type":"response.output_item.done","output_index":0,"item":5}`, true},
		{"modeled item missing member", `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"f","call_id":"c","name":"n"}}`, true},
		{"function_call added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"f","call_id":"c","name":"n","arguments":""}}`, false},
		{"function_call done needs status", `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"f","call_id":"c","name":"n","arguments":"{}"}}`, true},
		{"web_search done action", `{"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"w","status":"completed","action":{"type":"search","query":"q","sources":[{"type":"url","url":"u"}]}}}`, false},
		{"web_search action bad", `{"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"w","status":"completed","action":{"type":"search","sources":[{"type":"url"}]}}}`, true},
		{"annotation", `{"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","start_index":0,"end_index":1,"url":"u","title":"t"}}`, false},
		{"annotation unknown", `{"type":"response.output_text.annotation.added","annotation":{"type":"zzz"}}`, true},
		{"error flat", `{"type":"error","sequence_number":1,"message":"m"}`, false},
		{"error nested", `{"type":"error","sequence_number":1,"error":{"type":"t","message":"m"}}`, false},
		{"error invalid", `{"type":"error","message":"m"}`, true},
	} {
		if err := ValidateResponsesChunk([]byte(c.in)); (err != nil) != c.bad {
			t.Errorf("%s: %s: err = %v, want error %v", c.name, c.in, err, c.bad)
		}
	}
}

func TestUnmarshalChunkAcceptsAnyJSONNumber(t *testing.T) {
	var v struct {
		N int64 `json:"n"`
		M struct {
			K int `json:"k"`
		} `json:"m"`
	}
	if err := UnmarshalChunk([]byte(`{"n":1.5,"m":{"k":2.0}}`), &v); err != nil || v.N != 1 || v.M.K != 2 {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if err := UnmarshalChunk([]byte(`{"n":"x"}`), &v); err == nil {
		t.Fatal("string for int must still fail")
	}
}
