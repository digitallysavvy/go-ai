package bridgetest

import (
	"strings"
	"testing"

	"golang.org/x/net/websocket"
)

func TestStopDefaultsToEmptyObject(t *testing.T) {
	srv := NewServer(Options{Token: "tok"})
	defer srv.Close()
	ws, err := websocket.Dial(srv.URL(), "", "http://localhost/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()
	var hello string
	if err := websocket.Message.Receive(ws, &hello); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(ws, `{"type":"stop"}`); err != nil {
		t.Fatal(err)
	}
	var stop string
	if err := websocket.Message.Receive(ws, &stop); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stop, `"data":{}`) {
		t.Fatalf("stop frame = %s", stop)
	}
}
