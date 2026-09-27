package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/digitallysavvy/go-ai/pkg/workflow"
)

func main() {
	transport := &workflow.WorkflowChatTransport{}
	srv := httptest.NewServer(transport)
	defer srv.Close()

	var runID string
	client := workflow.NewWorkflowChatTransport(workflow.WorkflowChatTransportOptions{
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
