package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestIDGeneratorNextReturnsIncrementingIDs(t *testing.T) {
	gen := NewIDGenerator()

	firstRaw := gen.Next()
	first, ok := firstRaw.(uint64)
	if !ok {
		t.Fatalf("first ID type = %T, want uint64", firstRaw)
	}
	secondRaw := gen.Next()
	second, ok := secondRaw.(uint64)
	if !ok {
		t.Fatalf("second ID type = %T, want uint64", secondRaw)
	}

	if first != 1 || second != 2 {
		t.Fatalf("IDs = (%d, %d), want (1, 2)", first, second)
	}
}

func TestCreateRequestAndNotification(t *testing.T) {
	req, err := CreateRequest(uint64(7), "tools/list", map[string]interface{}{"cursor": "abc"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if req.JSONRpc != "2.0" || req.Method != "tools/list" || req.ID != uint64(7) {
		t.Fatalf("unexpected request: %#v", req)
	}
	if len(req.Params) == 0 {
		t.Fatal("expected marshaled params")
	}

	notif, err := CreateNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatalf("CreateNotification() error = %v", err)
	}
	if notif.ID != nil {
		t.Fatalf("notification ID = %#v, want nil", notif.ID)
	}
	if !IsNotification(notif) {
		t.Fatal("expected notification shape")
	}
}

func TestCreateRequestAndResponseMarshalErrors(t *testing.T) {
	_, err := CreateRequest(1, "x", map[string]interface{}{"bad": make(chan int)})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal params") {
		t.Fatalf("CreateRequest marshal error = %v", err)
	}

	_, err = CreateResponse(1, map[string]interface{}{"bad": make(chan int)})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal result") {
		t.Fatalf("CreateResponse marshal error = %v", err)
	}
}

func TestCreateResponseAndPredicates(t *testing.T) {
	resp, err := CreateResponse("req-1", map[string]interface{}{"ok": true})
	if err != nil {
		t.Fatalf("CreateResponse() error = %v", err)
	}
	if !IsResponse(resp) {
		t.Fatal("expected IsResponse=true")
	}
	if IsRequest(resp) || IsNotification(resp) {
		t.Fatal("response should not be request/notification")
	}

	req, _ := CreateRequest(1, "tools/list", nil)
	if !IsRequest(req) {
		t.Fatal("expected IsRequest=true")
	}
}

func TestCreateErrorResponseAndGetError(t *testing.T) {
	msg := CreateErrorResponse("abc", ErrorCodeInvalidParams, "bad params", map[string]interface{}{"field": "x"})
	if !IsError(msg) {
		t.Fatal("expected IsError=true")
	}
	if !IsResponse(msg) {
		t.Fatal("expected error message to be response")
	}

	err := GetError(msg)
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("GetError type = %T, want *MCPClientError", err)
	}
	if clientErr.Code != ErrorCodeInvalidParams || clientErr.Message != "bad params" {
		t.Fatalf("unexpected MCPClientError: %#v", clientErr)
	}

	if GetError(&MCPMessage{}) != nil {
		t.Fatal("GetError on non-error message should return nil")
	}
}

func TestParseParamsAndParseResult(t *testing.T) {
	msg := &MCPMessage{
		Params: []byte(`{"name":"weather","count":2}`),
		Result: []byte(`{"ok":true}`),
	}

	var params struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := ParseParams(msg, &params); err != nil {
		t.Fatalf("ParseParams() error = %v", err)
	}
	if params.Name != "weather" || params.Count != 2 {
		t.Fatalf("unexpected params: %#v", params)
	}

	var result struct {
		OK bool `json:"ok"`
	}
	if err := ParseResult(msg, &result); err != nil {
		t.Fatalf("ParseResult() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestParseParamsAndResultEmptyAndInvalid(t *testing.T) {
	msg := &MCPMessage{}
	var target map[string]interface{}
	if err := ParseParams(msg, &target); err != nil {
		t.Fatalf("ParseParams(empty) error = %v", err)
	}
	if err := ParseResult(msg, &target); err != nil {
		t.Fatalf("ParseResult(empty) error = %v", err)
	}

	msg.Params = []byte(`{"__proto__":"x"}`)
	if err := ParseParams(msg, &target); err != nil {
		t.Fatalf("expected ParseParams to preserve prototype-named own keys, got %v", err)
	}
	if target["__proto__"] != "x" {
		t.Fatalf("prototype-named key was not preserved: %#v", target)
	}

	msg.Result = []byte(`{"bad":`)
	if err := ParseResult(msg, &target); err == nil {
		t.Fatal("expected ParseResult invalid JSON error")
	}
}
