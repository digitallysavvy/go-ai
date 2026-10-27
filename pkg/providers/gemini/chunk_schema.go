package gemini

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/geminischema"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// The zod chunkSchema / responseSchema port lives in pkg/internal/geminischema
// (shared with the google batch result check). A frame that does not match
// becomes an `error` part (TypeValidationError in TS) and the stream keeps
// reading.
var zChunk = geminischema.Response

// zResponse is the doGenerate responseSchema.
var zResponse = geminischema.GenerateResponse

// chunkValidationMessage validates a JSON frame against the TS chunkSchema.
// It returns "" when the frame matches (or is not JSON, which is reported
// separately), otherwise the TS TypeValidationError message.
func chunkValidationMessage(data string) string {
	return geminischema.ValidationMessage(data)
}

// validationIssueText returns the issue list that follows "Error message: ".
func validationIssueText(message string) string { return zodlite.IssueText(message) }

func compactJSONString(data string) string { return zodlite.CompactJSON(data) }
