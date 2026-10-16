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

// TS createProviderExecutedToolFactory(supportsDeferredResults: true): the
// core only tracks deferred results for tools declared this way.
func TestCodeExecution20260120_ProviderExecutedDeferred(t *testing.T) {
	tool := CodeExecution20260120()
	if !tool.ProviderExecuted || !tool.SupportsDeferredResults {
		t.Fatalf("ProviderExecuted=%v SupportsDeferredResults=%v", tool.ProviderExecuted, tool.SupportsDeferredResults)
	}
}
