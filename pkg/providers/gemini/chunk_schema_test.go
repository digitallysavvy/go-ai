package gemini

import (
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Frames the TS chunkSchema rejects must become error parts (stream goes on).
func TestStreamSchemaMismatchIsErrorChunkAndStreamContinues(t *testing.T) {
	bad := []string{
		`[]`,
		`"x"`,
		`{"candidates":{}}`,
		`{"candidates":[1]}`,
		`{"candidates":[{"content":[]}]}`,
		`{"candidates":[{"content":{"parts":{}}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":5}]}}]}`,
		`{"candidates":[{"content":{"parts":["x"]}}]}`,
		`{"candidates":[{"content":{"parts":[{"thought":"yes"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"a"},"thought":1}]}}]}`,
		`{"candidates":[{"finishReason":5}]}`,
		`{"candidates":[{"safetyRatings":[{"blocked":"no"}]}]}`,
		`{"candidates":[{"groundingMetadata":{"webSearchQueries":"q"}}]}`,
		`{"candidates":[{"groundingMetadata":{"groundingChunks":[{"web":{"title":"t"}}]}}]}`,
		`{"candidates":[{"urlContextMetadata":{"urlMetadata":[{"retrievedUrl":"u"}]}}]}`,
		`{"usageMetadata":{"promptTokenCount":"3"}}`,
		`{"usageMetadata":{"promptTokensDetails":[{"modality":"TEXT"}]}}`,
		`{"promptFeedback":"blocked"}`,
		`{"responseId":3}`,
	}
	for _, b := range bad {
		s := newTestStream(sseStream(b, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
		var sawErr, sawText bool
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			switch c.Type {
			case provider.ChunkTypeError:
				sawErr = true
				if !strings.HasPrefix(c.Text, "Type validation failed: Value: ") || !strings.Contains(c.Text, ".\nError message: ") {
					t.Errorf("%s: message %q", b, c.Text)
				}
				if !providererrors.IsValidationError(c.Err) {
					t.Errorf("%s: want ValidationError, got %T", b, c.Err)
				}
			case provider.ChunkTypeText:
				sawText = true
			}
		}
		if !sawErr {
			t.Errorf("%s: no error chunk", b)
		}
		if !sawText {
			t.Errorf("%s: stream did not continue", b)
		}
	}
}

// Frames the TS chunkSchema accepts must not be rejected.
func TestChunkSchemaAcceptsValidFrames(t *testing.T) {
	good := []string{
		`{}`,
		`{"candidates":null,"usageMetadata":null,"promptFeedback":null,"responseId":null,"extra":1}`,
		`{"candidates":[{"content":{},"extra":true}]}`,
		`{"candidates":[{"content":null,"finishReason":null}]}`,
		`{"candidates":[{"content":{"parts":null}}]}`,
		`{"candidates":[{"content":{"parts":[{}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":null,"thought":null}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":null,"partialArgs":[{"jsonPath":"$.a","nullValue":null}]}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":"oops","text":"fallsThroughToLastVariant"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"toolCall":{"toolType":"T","id":"1","args":"s"}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"toolResponse":{"toolType":"T","id":"1","response":[1]}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"executableCode":null,"codeExecutionResult":{"outcome":"OK"}}]}}]}`,
		`{"candidates":[{"groundingMetadata":{"retrievalMetadata":{"anything":1},"groundingChunks":[{"web":{"uri":"u"},"image":null}]}}]}`,
		`{"usageMetadata":{"promptTokenCount":3,"unknown":{"x":1},"promptTokensDetails":[{"modality":"TEXT","tokenCount":3,"more":1}]}}`,
		`{"promptFeedback":{"blockReason":null,"safetyRatings":[{}]}}`,
	}
	for _, g := range good {
		if msg := chunkValidationMessage(g); msg != "" {
			t.Errorf("%s rejected: %s", g, msg)
		}
	}
}

func TestInvalidJSONFrameMessage(t *testing.T) {
	s := newTestStream(sseStream(`{not json`))
	c, _ := s.Next()
	if c == nil || c.Type != provider.ChunkTypeError || !strings.HasPrefix(c.Text, "JSON parsing failed: Text: {not json.\nError message: ") {
		t.Fatalf("got %+v", c)
	}
	var jpe *providererrors.JSONParseError
	if !errors.As(c.Err, &jpe) || jpe.Text != "{not json" {
		t.Fatalf("Err = %#v, want *JSONParseError", c.Err)
	}
}

func TestValidationIssueTextSafeWithoutMarker(t *testing.T) {
	if got := validationIssueText("none"); got != "none" {
		t.Fatalf("got %q", got)
	}
}

// Valid frames must also survive the typed decode and produce no error part.
func TestValidFramesProduceNoErrorChunk(t *testing.T) {
	good := []string{
		`{"candidates":[{"content":{"parts":[{"toolCall":{"toolType":"T","id":"1","args":"s"}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"toolResponse":{"toolType":"T","id":"1","response":[1]}}]}}]}`,
		`{"usageMetadata":{"promptTokenCount":3.5,"promptTokensDetails":[{"modality":"TEXT","tokenCount":2.5}]}}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":"oops","text":"x"}]}}]}`,
	}
	for _, g := range good {
		s := newTestStream(sseStream(g))
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				t.Errorf("%s: error chunk %q", g, c.Text)
			}
		}
	}
}
