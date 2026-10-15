package providerutils

import "testing"

func TestRequireChunkChoices(t *testing.T) {
	for in, wantErr := range map[string]bool{
		`{"choices":[]}`:             false,
		`{"choices":[{"delta":{}}]}`: false,
		`{"error":{"message":"x"}}`:  true,
		`{}`:                         true,
		`{"choices":null}`:           true,
		`{"choices":{"a":1}}`:        true,
		`not json`:                   true,
	} {
		if err := RequireChunkChoices([]byte(in)); (err != nil) != wantErr {
			t.Errorf("%s: err = %v, want error %v", in, err, wantErr)
		}
	}
}

func TestValidateChatChunk(t *testing.T) {
	type tc struct {
		in    string
		shape ChatChunkShape
		bad   bool
	}
	for _, c := range []tc{
		{`{"choices":[]}`, ChatChunkShape{}, false},
		{`{"choices":[{"delta":null}]}`, ChatChunkShape{}, false},
		{`{"choices":[{"delta":null}]}`, ChatChunkShape{DeltaRequired: true}, true},
		{`{"choices":[5]}`, ChatChunkShape{}, true},
		{`{"choices":[{"delta":"x"}]}`, ChatChunkShape{}, true},
		{`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"f"}}]}}]}`, ChatChunkShape{}, false},
		{`{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, ChatChunkShape{}, true},
		{`{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, ChatChunkShape{ToolCallFunctionOptional: true}, false},
		{`{"choices":[{"delta":{"tool_calls":"x"}}]}`, ChatChunkShape{}, true},
		{`{"choices":[{"text":"a"}]}`, ChatChunkShape{ChoiceTextRequired: true}, false},
		{`{"choices":[{"delta":{}}]}`, ChatChunkShape{ChoiceTextRequired: true}, true},
		{`{"choices":[{"text":5}]}`, ChatChunkShape{ChoiceTextRequired: true}, true},
	} {
		if err := ValidateChatChunk([]byte(c.in), c.shape); (err != nil) != c.bad {
			t.Errorf("%s %+v: err = %v, want error %v", c.in, c.shape, err, c.bad)
		}
	}
}
