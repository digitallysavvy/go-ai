package tools

import "testing"

func TestUnmarshalCodeExecutionResult_Encrypted(t *testing.T) {
	r, err := UnmarshalCodeExecutionResult([]byte(`{"type":"encrypted_code_execution_result","encrypted_stdout":"abc","stderr":"","return_code":0,"content":[{"type":"code_execution_output","file_id":"f1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := r.(*EncryptedProgrammaticExecutionResult)
	if !ok || e.EncryptedStdout != "abc" || len(e.Content) != 1 || e.Content[0].FileID != "f1" {
		t.Fatalf("got %#v", r)
	}
}
