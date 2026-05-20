package ai

import "strings"

func appendSandboxDescription(system string, sandbox interface{}) string {
	sb, ok := sandbox.(Sandbox)
	if !ok || sb == nil {
		return system
	}
	desc := strings.TrimSpace(sb.Description())
	if desc == "" {
		return system
	}
	if strings.TrimSpace(system) == "" {
		return desc
	}
	return system + "\n\n" + desc
}
