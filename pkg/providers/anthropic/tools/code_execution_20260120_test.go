package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Tool structure tests (ACODE-T07)
// ============================================================================

func TestCodeExecution20260120_Basic(t *testing.T) {
	tool := CodeExecution20260120()

	assert.Equal(t, codeExecution20260120ToolName, tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestCodeExecution20260120_Constants(t *testing.T) {
	assert.Equal(t, "code-execution_20260120", AnthropicCodeExecutionToolType)
	assert.Equal(t, "code-execution-20260120", BetaHeaderCodeExecution)
}

func TestCodeExecution20260120_Schema(t *testing.T) {
	tool := CodeExecution20260120()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok, "Parameters must be a map")

	oneOf, ok := params["oneOf"].([]interface{})
	require.True(t, ok, "Parameters must have oneOf")
	assert.Len(t, oneOf, 3, "Must have 3 input types: programmatic, bash, text_editor")

	discriminator, ok := params["discriminator"].(map[string]interface{})
	require.True(t, ok, "Parameters must have discriminator")
	assert.Equal(t, "type", discriminator["propertyName"])
}

func TestCodeExecution20260120_ProgrammaticSchema(t *testing.T) {
	tool := CodeExecution20260120()
	params := tool.Parameters.(map[string]interface{})
	oneOf := params["oneOf"].([]interface{})

	programmatic, ok := oneOf[0].(map[string]interface{})
	require.True(t, ok)

	properties, ok := programmatic["properties"].(map[string]interface{})
	require.True(t, ok)

	typeField := properties["type"].(map[string]interface{})
	assert.Equal(t, CodeExecutionInputTypeProgrammatic, typeField["const"])

	required := programmatic["required"].([]string)
	assert.Contains(t, required, "type")
	assert.Contains(t, required, "code")
}

func TestCodeExecution20260120_BashSchema(t *testing.T) {
	tool := CodeExecution20260120()
	params := tool.Parameters.(map[string]interface{})
	oneOf := params["oneOf"].([]interface{})

	bash, ok := oneOf[1].(map[string]interface{})
	require.True(t, ok)

	properties, ok := bash["properties"].(map[string]interface{})
	require.True(t, ok)

	typeField := properties["type"].(map[string]interface{})
	assert.Equal(t, CodeExecutionInputTypeBash, typeField["const"])

	required := bash["required"].([]string)
	assert.Contains(t, required, "type")
	assert.Contains(t, required, "command")
}

func TestCodeExecution20260120_TextEditorSchema(t *testing.T) {
	tool := CodeExecution20260120()
	params := tool.Parameters.(map[string]interface{})
	oneOf := params["oneOf"].([]interface{})

	textEditor, ok := oneOf[2].(map[string]interface{})
	require.True(t, ok)

	textEditorOneOf, ok := textEditor["oneOf"].([]interface{})
	require.True(t, ok, "Text editor must have nested oneOf")
	assert.Len(t, textEditorOneOf, 3, "Must have 3 text editor commands: view, create, str_replace")
}

// ============================================================================
// JSON round-trip tests for input types (ACODE-T04, ACODE-T06)
// ============================================================================

func TestUnmarshalCodeExecutionInput_Programmatic(t *testing.T) {
	input := &ProgrammaticToolCallInput{
		Type: CodeExecutionInputTypeProgrammatic,
		Code: "print('hello world')",
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := UnmarshalCodeExecutionInput(data)
	require.NoError(t, err)

	got, ok := result.(*ProgrammaticToolCallInput)
	require.True(t, ok, "Expected *ProgrammaticToolCallInput")
	assert.Equal(t, CodeExecutionInputTypeProgrammatic, got.Type)
	assert.Equal(t, "print('hello world')", got.Code)
	assert.Equal(t, CodeExecutionInputTypeProgrammatic, got.GetInputType())
}

func TestUnmarshalCodeExecutionInput_Bash(t *testing.T) {
	input := &BashCodeExecutionInput{
		Type:    CodeExecutionInputTypeBash,
		Command: "ls -la /tmp",
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := UnmarshalCodeExecutionInput(data)
	require.NoError(t, err)

	got, ok := result.(*BashCodeExecutionInput)
	require.True(t, ok, "Expected *BashCodeExecutionInput")
	assert.Equal(t, CodeExecutionInputTypeBash, got.Type)
	assert.Equal(t, "ls -la /tmp", got.Command)
	assert.Equal(t, CodeExecutionInputTypeBash, got.GetInputType())
}

func TestUnmarshalCodeExecutionInput_TextEditorView(t *testing.T) {
	input := &TextEditorInput{
		Type:    CodeExecutionInputTypeTextEditor,
		Command: "view",
		Path:    "/etc/hosts",
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := UnmarshalCodeExecutionInput(data)
	require.NoError(t, err)

	got, ok := result.(*TextEditorInput)
	require.True(t, ok, "Expected *TextEditorInput")
	assert.Equal(t, CodeExecutionInputTypeTextEditor, got.Type)
	assert.Equal(t, "view", got.Command)
	assert.Equal(t, "/etc/hosts", got.Path)
	assert.Nil(t, got.FileText)
	assert.Nil(t, got.OldStr)
	assert.Nil(t, got.NewStr)
}

func TestUnmarshalCodeExecutionInput_TextEditorCreate(t *testing.T) {
	fileText := "hello world\n"
	input := &TextEditorInput{
		Type:     CodeExecutionInputTypeTextEditor,
		Command:  "create",
		Path:     "/tmp/test.txt",
		FileText: &fileText,
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := UnmarshalCodeExecutionInput(data)
	require.NoError(t, err)

	got, ok := result.(*TextEditorInput)
	require.True(t, ok)
	assert.Equal(t, "create", got.Command)
	assert.Equal(t, "/tmp/test.txt", got.Path)
	require.NotNil(t, got.FileText)
	assert.Equal(t, "hello world\n", *got.FileText)
}

func TestUnmarshalCodeExecutionInput_TextEditorStrReplace(t *testing.T) {
	oldStr := "foo"
	newStr := "bar"
	input := &TextEditorInput{
		Type:    CodeExecutionInputTypeTextEditor,
		Command: "str_replace",
		Path:    "/tmp/test.txt",
		OldStr:  &oldStr,
		NewStr:  &newStr,
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := UnmarshalCodeExecutionInput(data)
	require.NoError(t, err)

	got, ok := result.(*TextEditorInput)
	require.True(t, ok)
	assert.Equal(t, "str_replace", got.Command)
	require.NotNil(t, got.OldStr)
	require.NotNil(t, got.NewStr)
	assert.Equal(t, "foo", *got.OldStr)
	assert.Equal(t, "bar", *got.NewStr)
}

func TestUnmarshalCodeExecutionInput_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown_type","foo":"bar"}`)
	_, err := UnmarshalCodeExecutionInput(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

func TestUnmarshalCodeExecutionInput_InvalidJSON(t *testing.T) {
	_, err := UnmarshalCodeExecutionInput([]byte(`{invalid`))
	assert.Error(t, err)
}

// ============================================================================
// JSON round-trip tests for result types (ACODE-T06)
// ============================================================================

func TestUnmarshalCodeExecutionResult_Programmatic(t *testing.T) {
	result := &ProgrammaticExecutionResult{
		Type:       CodeExecutionResultTypeProgrammatic,
		Stdout:     "42\n",
		Stderr:     "",
		ReturnCode: 0,
		Content: []CodeExecutionOutputItem{
			{Type: "code_execution_output", FileID: "file-abc123"},
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*ProgrammaticExecutionResult)
	require.True(t, ok, "Expected *ProgrammaticExecutionResult")
	assert.Equal(t, CodeExecutionResultTypeProgrammatic, r.Type)
	assert.Equal(t, "42\n", r.Stdout)
	assert.Equal(t, 0, r.ReturnCode)
	assert.Len(t, r.Content, 1)
	assert.Equal(t, "file-abc123", r.Content[0].FileID)
	assert.Equal(t, CodeExecutionResultTypeProgrammatic, r.GetResultType())
}

func TestUnmarshalCodeExecutionResult_Bash(t *testing.T) {
	result := &BashExecutionResult{
		Type:       CodeExecutionResultTypeBash,
		Stdout:     "hello\n",
		Stderr:     "",
		ReturnCode: 0,
		Content: []BashCodeExecutionOutputItem{
			{Type: "bash_code_execution_output", FileID: "file-xyz"},
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*BashExecutionResult)
	require.True(t, ok, "Expected *BashExecutionResult")
	assert.Equal(t, CodeExecutionResultTypeBash, r.Type)
	assert.Equal(t, "hello\n", r.Stdout)
	assert.Equal(t, 0, r.ReturnCode)
	assert.Len(t, r.Content, 1)
}

func TestUnmarshalCodeExecutionResult_BashError(t *testing.T) {
	result := &BashExecutionError{
		Type:      CodeExecutionResultTypeBashError,
		ErrorCode: "execution_time_exceeded",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*BashExecutionError)
	require.True(t, ok, "Expected *BashExecutionError")
	assert.Equal(t, CodeExecutionResultTypeBashError, r.Type)
	assert.Equal(t, "execution_time_exceeded", r.ErrorCode)
	assert.Equal(t, CodeExecutionResultTypeBashError, r.GetResultType())
}

func TestUnmarshalCodeExecutionResult_TextEditorError(t *testing.T) {
	result := &TextEditorExecutionError{
		Type:      CodeExecutionResultTypeTextEditorError,
		ErrorCode: "file_not_found",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorExecutionError)
	require.True(t, ok, "Expected *TextEditorExecutionError")
	assert.Equal(t, "file_not_found", r.ErrorCode)
}

func TestUnmarshalCodeExecutionResult_ViewResult(t *testing.T) {
	numLines := 42
	startLine := 1
	totalLines := 42
	result := &TextEditorViewResult{
		Type:       CodeExecutionResultTypeViewResult,
		Content:    "line1\nline2\n",
		FileType:   "text",
		NumLines:   &numLines,
		StartLine:  &startLine,
		TotalLines: &totalLines,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorViewResult)
	require.True(t, ok, "Expected *TextEditorViewResult")
	assert.Equal(t, "line1\nline2\n", r.Content)
	assert.Equal(t, "text", r.FileType)
	require.NotNil(t, r.NumLines)
	assert.Equal(t, 42, *r.NumLines)
}

func TestUnmarshalCodeExecutionResult_ViewResult_NullableFields(t *testing.T) {
	result := &TextEditorViewResult{
		Type:       CodeExecutionResultTypeViewResult,
		Content:    "data",
		FileType:   "image",
		NumLines:   nil,
		StartLine:  nil,
		TotalLines: nil,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorViewResult)
	require.True(t, ok)
	assert.Nil(t, r.NumLines)
	assert.Nil(t, r.StartLine)
	assert.Nil(t, r.TotalLines)
}

func TestUnmarshalCodeExecutionResult_CreateResult(t *testing.T) {
	result := &TextEditorCreateResult{
		Type:         CodeExecutionResultTypeCreateResult,
		IsFileUpdate: true,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorCreateResult)
	require.True(t, ok, "Expected *TextEditorCreateResult")
	assert.True(t, r.IsFileUpdate)
}

func TestUnmarshalCodeExecutionResult_StrReplaceResult(t *testing.T) {
	newLines := 3
	newStart := 10
	oldLines := 2
	oldStart := 10
	result := &TextEditorStrReplaceResult{
		Type:     CodeExecutionResultTypeStrReplaceResult,
		Lines:    []string{"new line 1", "new line 2", "new line 3"},
		NewLines: &newLines,
		NewStart: &newStart,
		OldLines: &oldLines,
		OldStart: &oldStart,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorStrReplaceResult)
	require.True(t, ok, "Expected *TextEditorStrReplaceResult")
	assert.Len(t, r.Lines, 3)
	require.NotNil(t, r.NewLines)
	assert.Equal(t, 3, *r.NewLines)
}

func TestUnmarshalCodeExecutionResult_StrReplaceResult_NullFields(t *testing.T) {
	result := &TextEditorStrReplaceResult{
		Type:     CodeExecutionResultTypeStrReplaceResult,
		Lines:    nil,
		NewLines: nil,
		NewStart: nil,
		OldLines: nil,
		OldStart: nil,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	got, err := UnmarshalCodeExecutionResult(data)
	require.NoError(t, err)

	r, ok := got.(*TextEditorStrReplaceResult)
	require.True(t, ok)
	assert.Nil(t, r.Lines)
	assert.Nil(t, r.NewLines)
}

func TestUnmarshalCodeExecutionResult_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown_result_type"}`)
	_, err := UnmarshalCodeExecutionResult(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

func TestUnmarshalCodeExecutionResult_InvalidJSON(t *testing.T) {
	_, err := UnmarshalCodeExecutionResult([]byte(`{invalid`))
	assert.Error(t, err)
}

// ============================================================================
// Type discriminator verification (ACODE-T04)
// ============================================================================

func TestCodeExecutionInput_TypeDiscriminators(t *testing.T) {
	tests := []struct {
		name     string
		input    CodeExecutionInput
		wantType string
	}{
		{
			name:     "programmatic",
			input:    &ProgrammaticToolCallInput{Type: CodeExecutionInputTypeProgrammatic, Code: "x=1"},
			wantType: "programmatic-tool-call",
		},
		{
			name:     "bash",
			input:    &BashCodeExecutionInput{Type: CodeExecutionInputTypeBash, Command: "echo hi"},
			wantType: "bash_code_execution",
		},
		{
			name:     "text_editor",
			input:    &TextEditorInput{Type: CodeExecutionInputTypeTextEditor, Command: "view", Path: "/tmp"},
			wantType: "text_editor_code_execution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantType, tt.input.GetInputType())

			// Verify marshal produces correct type field
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)

			var disc struct {
				Type string `json:"type"`
			}
			require.NoError(t, json.Unmarshal(data, &disc))
			assert.Equal(t, tt.wantType, disc.Type)
		})
	}
}

func TestCodeExecutionResult_TypeDiscriminators(t *testing.T) {
	tests := []struct {
		name     string
		result   CodeExecutionResult
		wantType string
	}{
		{
			name:     "programmatic",
			result:   &ProgrammaticExecutionResult{Type: CodeExecutionResultTypeProgrammatic},
			wantType: "code_execution_result",
		},
		{
			name:     "bash",
			result:   &BashExecutionResult{Type: CodeExecutionResultTypeBash},
			wantType: "bash_code_execution_result",
		},
		{
			name:     "bash_error",
			result:   &BashExecutionError{Type: CodeExecutionResultTypeBashError},
			wantType: "bash_code_execution_tool_result_error",
		},
		{
			name:     "text_editor_error",
			result:   &TextEditorExecutionError{Type: CodeExecutionResultTypeTextEditorError},
			wantType: "text_editor_code_execution_tool_result_error",
		},
		{
			name:     "view_result",
			result:   &TextEditorViewResult{Type: CodeExecutionResultTypeViewResult},
			wantType: "text_editor_code_execution_view_result",
		},
		{
			name:     "create_result",
			result:   &TextEditorCreateResult{Type: CodeExecutionResultTypeCreateResult},
			wantType: "text_editor_code_execution_create_result",
		},
		{
			name:     "str_replace_result",
			result:   &TextEditorStrReplaceResult{Type: CodeExecutionResultTypeStrReplaceResult},
			wantType: "text_editor_code_execution_str_replace_result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantType, tt.result.GetResultType())

			data, err := json.Marshal(tt.result)
			require.NoError(t, err)

			var disc struct {
				Type string `json:"type"`
			}
			require.NoError(t, json.Unmarshal(data, &disc))
			assert.Equal(t, tt.wantType, disc.Type)
		})
	}
}

// ============================================================================
// Error code tests (ACODE-T14)
// ============================================================================

func TestBashExecutionError_ErrorCodes(t *testing.T) {
	errorCodes := []string{
		"invalid_tool_input",
		"unavailable",
		"too_many_requests",
		"execution_time_exceeded",
		"output_file_too_large",
	}

	for _, code := range errorCodes {
		t.Run(code, func(t *testing.T) {
			errResult := &BashExecutionError{
				Type:      CodeExecutionResultTypeBashError,
				ErrorCode: code,
			}
			data, err := json.Marshal(errResult)
			require.NoError(t, err)

			got, err := UnmarshalCodeExecutionResult(data)
			require.NoError(t, err)

			r := got.(*BashExecutionError)
			assert.Equal(t, code, r.ErrorCode)
		})
	}
}

func TestTextEditorExecutionError_ErrorCodes(t *testing.T) {
	errorCodes := []string{
		"invalid_tool_input",
		"unavailable",
		"too_many_requests",
		"execution_time_exceeded",
		"file_not_found",
	}

	for _, code := range errorCodes {
		t.Run(code, func(t *testing.T) {
			errResult := &TextEditorExecutionError{
				Type:      CodeExecutionResultTypeTextEditorError,
				ErrorCode: code,
			}
			data, err := json.Marshal(errResult)
			require.NoError(t, err)

			got, err := UnmarshalCodeExecutionResult(data)
			require.NoError(t, err)

			r := got.(*TextEditorExecutionError)
			assert.Equal(t, code, r.ErrorCode)
		})
	}
}

// ============================================================================
// Response parsing tests (ACODE-T11, ACODE-T12, ACODE-T13, ACODE-T15)
// ============================================================================

func TestParseModelResponse_AllExecutionModes(t *testing.T) {
	// Simulate what the model returns as tool call input JSON
	tests := []struct {
		name     string
		json     string
		wantType string
	}{
		{
			name:     "programmatic mode",
			json:     `{"type":"programmatic-tool-call","code":"import math\nprint(math.pi)"}`,
			wantType: CodeExecutionInputTypeProgrammatic,
		},
		{
			name:     "bash mode",
			json:     `{"type":"bash_code_execution","command":"echo hello"}`,
			wantType: CodeExecutionInputTypeBash,
		},
		{
			name:     "text editor view mode",
			json:     `{"type":"text_editor_code_execution","command":"view","path":"/etc/hosts"}`,
			wantType: CodeExecutionInputTypeTextEditor,
		},
		{
			name:     "text editor create mode",
			json:     `{"type":"text_editor_code_execution","command":"create","path":"/tmp/test.txt","file_text":"content"}`,
			wantType: CodeExecutionInputTypeTextEditor,
		},
		{
			name:     "text editor str_replace mode",
			json:     `{"type":"text_editor_code_execution","command":"str_replace","path":"/tmp/test.txt","old_str":"foo","new_str":"bar"}`,
			wantType: CodeExecutionInputTypeTextEditor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := UnmarshalCodeExecutionInput([]byte(tt.json))
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, input.GetInputType())
		})
	}
}

func TestParseExecutionResults_AllResultTypes(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantType string
	}{
		{
			name:     "programmatic result",
			json:     `{"type":"code_execution_result","stdout":"42\n","stderr":"","return_code":0,"content":[]}`,
			wantType: CodeExecutionResultTypeProgrammatic,
		},
		{
			name:     "bash result",
			json:     `{"type":"bash_code_execution_result","stdout":"hello\n","stderr":"","return_code":0,"content":[]}`,
			wantType: CodeExecutionResultTypeBash,
		},
		{
			name:     "bash error",
			json:     `{"type":"bash_code_execution_tool_result_error","error_code":"unavailable"}`,
			wantType: CodeExecutionResultTypeBashError,
		},
		{
			name:     "text editor error",
			json:     `{"type":"text_editor_code_execution_tool_result_error","error_code":"file_not_found"}`,
			wantType: CodeExecutionResultTypeTextEditorError,
		},
		{
			name:     "view result",
			json:     `{"type":"text_editor_code_execution_view_result","content":"file contents","file_type":"text","num_lines":10,"start_line":1,"total_lines":10}`,
			wantType: CodeExecutionResultTypeViewResult,
		},
		{
			name:     "create result",
			json:     `{"type":"text_editor_code_execution_create_result","is_file_update":false}`,
			wantType: CodeExecutionResultTypeCreateResult,
		},
		{
			name:     "str_replace result",
			json:     `{"type":"text_editor_code_execution_str_replace_result","lines":["new line"],"new_lines":1,"new_start":5,"old_lines":1,"old_start":5}`,
			wantType: CodeExecutionResultTypeStrReplaceResult,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := UnmarshalCodeExecutionResult([]byte(tt.json))
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, result.GetResultType())
		})
	}
}
