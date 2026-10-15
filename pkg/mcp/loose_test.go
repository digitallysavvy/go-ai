package mcp

import (
	"encoding/json"
	"testing"
)

// TS parity: z.looseObject / .loose() keep keys the schema does not name.
func TestLooseKeysSurviveDecodeAndEncode(t *testing.T) {
	var res ListResourcesResult
	in := `{"resources":[{"uri":"u","name":"n","icons":[{"src":"x"}],"annotations":{"audience":["user"]}}]}`
	if err := json.Unmarshal([]byte(in), &res); err != nil {
		t.Fatal(err)
	}
	r := res.Resources[0]
	if r.Extra["annotations"] == nil || r.Extra["icons"] == nil {
		t.Fatalf("resource extras lost: %#v", r.Extra)
	}
	out, _ := json.Marshal(r)
	if string(out) != `{"uri":"u","name":"n","annotations":{"audience":["user"]},"icons":[{"src":"x"}]}` {
		t.Fatalf("resource = %s", out)
	}

	var init InitializeResult
	cap := `{"protocolVersion":"v","capabilities":{"tasks":{"x":1},"tools":{"listChanged":true,"extra":2},"resources":{"subscribe":true,"template":true},"logging":{"a":1}},"serverInfo":{"name":"s","version":"1","websiteUrl":"w"}}`
	if err := json.Unmarshal([]byte(cap), &init); err != nil {
		t.Fatal(err)
	}
	c := init.Capabilities
	if c.Extra["tasks"] == nil || c.Tools.Extra()["extra"] == nil || c.Resources.Extra()["template"] == nil ||
		c.Logging.Extra()["a"] == nil || init.ServerInfo.Extra()["websiteUrl"] == nil {
		t.Fatalf("capability extras lost: %#v", c)
	}
	out, _ = json.Marshal(c)
	if string(out) != `{"logging":{"a":1},"resources":{"subscribe":true,"template":true},"tools":{"listChanged":true,"extra":2},"tasks":{"x":1}}` {
		t.Fatalf("capabilities = %s", out)
	}

	var rc ReadResourceResult
	if err := json.Unmarshal([]byte(`{"contents":[{"uri":"u","text":"","size":3}]}`), &rc); err != nil {
		t.Fatal(err)
	}
	out, _ = json.Marshal(rc.Contents[0])
	if string(out) != `{"uri":"u","text":"","size":3}` {
		t.Fatalf("content = %s", out)
	}

	var tool MCPTool
	if err := json.Unmarshal([]byte(`{"name":"t","inputSchema":{},"icons":[1]}`), &tool); err != nil {
		t.Fatal(err)
	}
	if tool.Extra["icons"] == nil {
		t.Fatal("tool extras lost")
	}
	var p MCPPrompt
	if err := json.Unmarshal([]byte(`{"name":"p","arguments":[{"name":"a","x":1}],"icons":[]}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Extra["icons"] == nil || p.Arguments[0].Extra()["x"] == nil {
		t.Fatalf("prompt extras lost: %#v", p)
	}
}
