package gateway

import (
	"errors"
	"strings"
	"testing"

	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS reads the video SSE stream through parseJsonEventStream
// (EventSourceParserStream): an event's data lines are joined with "\n", the
// '[DONE]' event is skipped, and an event with no closing blank line at EOF is
// never dispatched.
func TestVideoSSEEventsParsedLikeEventSourceParser(t *testing.T) {
	const errorEvent = `{"type":"error","message":"slow down","errorType":"rate_limit_exceeded","statusCode":429,"param":null}`
	for name, sse := range map[string]string{
		"multi-line data": "data: {\"type\":\"error\",\"message\":\"slow down\",\n" +
			"data: \"errorType\":\"rate_limit_exceeded\",\"statusCode\":429,\"param\":null}\n\n",
		"[DONE] skipped": "data: [DONE]\n\ndata: " + errorEvent + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := generateWithSSE(t, sse)
			var rl *gatewayerrors.GatewayRateLimitError
			if !errors.As(err, &rl) {
				t.Fatalf("err = %T %v", err, err)
			}
		})
	}

	t.Run("event without closing blank line", func(t *testing.T) {
		err := generateWithSSE(t, "data: "+errorEvent+"\n")
		var re *gatewayerrors.GatewayResponseError
		// As "no data event": TS's 'SSE stream ended without a data event'
		// APICallError, which asGatewayError reports with the stream's status.
		if !errors.As(err, &re) || re.GetStatusCode() != 200 || !strings.Contains(re.Error(), "Invalid error response format") {
			t.Fatalf("err = %T %v", err, err)
		}
	})
}
