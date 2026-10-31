package cohere

import "testing"

func TestValidateCohereChunk(t *testing.T) {
	for in, bad := range map[string]bool{
		`{"type":"citation-start"}`:        false,
		`{"type":"tool-call-end"}`:         false,
		`{"type":"message-start"}`:         false,
		`{"type":"message-start","id":5}`:  true,
		`{"type":"content-end","index":0}`: false,
		`{"type":"content-end"}`:           true,
		`{"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text","text":""}}}}`:                                      false,
		`{"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text"}}}}`:                                                true,
		`{"type":"content-delta","index":0,"delta":{"message":{"content":{"thinking":"x"}}}}`:                                               false,
		`{"type":"content-delta","index":0,"delta":{"message":{"content":{}}}}`:                                                             true,
		`{"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":1,"output_tokens":2}}}}`:               false,
		`{"type":"message-end","delta":{"finish_reason":"COMPLETE"}}`:                                                                       true,
		`{"type":"tool-plan-delta","delta":{"message":{"tool_plan":"p"}}}`:                                                                  false,
		`{"type":"tool-call-start","delta":{"message":{"tool_calls":{"id":"a","type":"function","function":{"name":"n","arguments":""}}}}}`: false,
		`{"type":"tool-call-start","delta":{"message":{"tool_calls":{"id":"a","function":{"name":"n","arguments":""}}}}}`:                   true,
		`{"type":"tool-call-delta","delta":{"message":{"tool_calls":{"function":{"arguments":"{"}}}}}`:                                      false,
		`{"type":"tool-call-delta","delta":{}}`:                                                                                             true,
		`{"type":"surprise"}`:                                                                                                               true,
		`{"foo":1}`:                                                                                                                         true,
	} {
		if err := validateCohereChunk([]byte(in)); (err != nil) != bad {
			t.Errorf("%s: err = %v, want error %v", in, err, bad)
		}
	}
}
