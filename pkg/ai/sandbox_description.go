package ai

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func appendSandboxDescription(system string, sandbox interface{}) string {
	sb, ok := sandbox.(providerutils.SandboxSession)
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
