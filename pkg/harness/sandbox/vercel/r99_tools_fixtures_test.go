package vercel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// r99toolsRealistic completes a test response body with the fields the
// @vercel/sandbox 3.9.0 response schemas require (validators.js) and that a
// real API response always carries: fixtures written before the client
// validated responses left them out. Present values are never changed, so a
// fixture that sends a wrong type still fails validation. A session or
// sandbox networkPolicy in the request shape (a mode string, or the
// allow/subnets object) is rewritten to the API response shape ({mode} or
// the legacy {mode: "custom", allowedDomains, allowedCIDRs, deniedCIDRs}).
func r99toolsRealistic(body []byte) []byte {
	// JSON Lines: each record on its own.
	if lines := bytes.Split(body, []byte("\n")); len(lines) > 1 {
		for i, line := range lines {
			if len(bytes.TrimSpace(line)) > 0 {
				lines[i] = r99toolsRealisticRecord(line)
			}
		}
		return bytes.Join(lines, []byte("\n"))
	}
	return r99toolsRealisticRecord(body)
}

func r99toolsRealisticRecord(body []byte) []byte {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return body
	}
	sessionID := "sess"
	if s, ok := obj["session"].(map[string]interface{}); ok {
		if id, ok := s["id"].(string); ok {
			sessionID = id
		}
	}
	for key, value := range obj {
		switch key {
		case "session":
			r99toolsFillSession(value)
		case "sessions":
			for _, s := range r99toolsList(value) {
				r99toolsFillSession(s)
			}
		case "sandbox":
			r99toolsFillSandbox(value, sessionID)
		case "snapshot":
			r99toolsFillSnapshot(value, sessionID)
		case "snapshots":
			for _, s := range r99toolsList(value) {
				r99toolsFillSnapshot(s, sessionID)
			}
		case "command":
			r99toolsFillCommand(value, sessionID)
		case "pagination":
			if p, ok := value.(map[string]interface{}); ok {
				r99toolsDefault(p, "count", float64(0))
				r99toolsDefault(p, "next", nil)
			}
		}
	}
	if _, ok := obj["sandbox"]; ok {
		if _, ok := obj["session"]; ok {
			r99toolsDefault(obj, "routes", []interface{}{})
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

func r99toolsList(v interface{}) []interface{} {
	list, _ := v.([]interface{})
	return list
}

func r99toolsDefault(m map[string]interface{}, key string, value interface{}) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}

func r99toolsFillSession(v interface{}) {
	s, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	r99toolsDefault(s, "id", "sess")
	r99toolsDefault(s, "memory", float64(2048))
	r99toolsDefault(s, "vcpus", float64(1))
	r99toolsDefault(s, "region", "iad1")
	r99toolsDefault(s, "timeout", float64(300000))
	r99toolsDefault(s, "status", "running")
	r99toolsDefault(s, "requestedAt", float64(1))
	r99toolsDefault(s, "createdAt", float64(1))
	r99toolsDefault(s, "cwd", "/vercel/sandbox")
	r99toolsDefault(s, "updatedAt", float64(1))
	r99toolsAPIPolicy(s)
}

func r99toolsFillSandbox(v interface{}, sessionID string) {
	s, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	r99toolsDefault(s, "name", "sb")
	r99toolsDefault(s, "persistent", false)
	r99toolsDefault(s, "createdAt", float64(1))
	r99toolsDefault(s, "updatedAt", float64(1))
	r99toolsDefault(s, "currentSessionId", sessionID)
	r99toolsDefault(s, "status", "running")
	r99toolsAPIPolicy(s)
}

func r99toolsFillSnapshot(v interface{}, sessionID string) {
	s, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	r99toolsDefault(s, "id", "snap")
	r99toolsDefault(s, "sourceSessionId", sessionID)
	r99toolsDefault(s, "region", "iad1")
	r99toolsDefault(s, "status", "created")
	r99toolsDefault(s, "sizeBytes", float64(0))
	r99toolsDefault(s, "createdAt", float64(1))
	r99toolsDefault(s, "updatedAt", float64(1))
}

func r99toolsFillCommand(v interface{}, sessionID string) {
	c, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	r99toolsDefault(c, "id", "cmd")
	r99toolsDefault(c, "name", "sh")
	if args, ok := c["args"]; !ok || args == nil {
		c["args"] = []interface{}{}
	}
	if cwd, ok := c["cwd"]; !ok || cwd == "" {
		c["cwd"] = "/vercel/sandbox"
	}
	if id, ok := c["sessionId"]; !ok || id == "" {
		c["sessionId"] = sessionID
	}
	r99toolsDefault(c, "exitCode", nil)
	r99toolsDefault(c, "startedAt", float64(1))
}

// r99toolsAPIPolicy rewrites a networkPolicy in the SDK request shape to the
// shape the API responds with.
func r99toolsAPIPolicy(m map[string]interface{}) {
	switch p := m["networkPolicy"].(type) {
	case string:
		m["networkPolicy"] = map[string]interface{}{"mode": p}
	case map[string]interface{}:
		if _, ok := p["mode"]; ok {
			return
		}
		legacy := map[string]interface{}{"mode": "custom"}
		if allow, ok := p["allow"].([]interface{}); ok {
			legacy["allowedDomains"] = allow
		} else if allow, ok := p["allow"].(map[string]interface{}); ok {
			domains := []interface{}{}
			for domain := range allow {
				domains = append(domains, domain)
			}
			legacy["allowedDomains"] = domains
		}
		if subnets, ok := p["subnets"].(map[string]interface{}); ok {
			if a, ok := subnets["allow"]; ok {
				legacy["allowedCIDRs"] = a
			}
			if d, ok := subnets["deny"]; ok {
				legacy["deniedCIDRs"] = d
			}
		}
		m["networkPolicy"] = legacy
	}
}

// r99toolsWrite writes a test response completed by r99toolsRealistic.
func r99toolsWrite(w io.Writer, body string) (int, error) {
	return w.Write(r99toolsRealistic([]byte(body)))
}

// r99toolsWritef is r99toolsWrite of fmt.Sprintf(format, args...).
func r99toolsWritef(w io.Writer, format string, args ...interface{}) (int, error) {
	return r99toolsWrite(w, fmt.Sprintf(format, args...))
}
