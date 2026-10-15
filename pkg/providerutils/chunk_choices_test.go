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
