package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/digitallysavvy/go-ai/pkg/workflow"
)

// This example demonstrates the server-side run multiplexer
// (workflow.WorkflowRunMultiplexer): it serves run-scoped SSE and supports
// resuming a run. For the client-side transport that implements
// ai.ChatTransport (the Go port of TS WorkflowChatTransport), see
// workflow.NewWorkflowChatTransport.
func main() {
	transport := &workflow.WorkflowRunMultiplexer{}
	srv := httptest.NewServer(transport)
	defer srv.Close()

	var runID string
	client := workflow.NewWorkflowRunMultiplexer(workflow.WorkflowRunMultiplexerOptions{
		API: srv.URL,
		OnChatSendMessage: func(_ *http.Response, _ workflow.SendMessagesOptions) error {
			return nil
		},
		OnChatEnd: func(e workflow.ChatEndEvent) error {
			if e.RunID != "" {
				runID = e.RunID
			}
			return nil
		},
	})

	events, err := client.SendMessages(context.Background(), workflow.SendMessagesOptions{
		ChatID:   "chat-1",
		Trigger:  "submit-message",
		Messages: []map[string]string{{"role": "user", "content": "hello"}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("received events:", len(events))

	if runID != "" {
		replayed, err := client.ReconnectToStream(context.Background(), workflow.ReconnectToStreamOptions{
			RunID:      runID,
			ChatID:     "chat-1",
			StartIndex: -1,
		})
		if err != nil {
			panic(err)
		}
		fmt.Println("replayed events:", len(replayed))
	}
}
