package responses

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// toolConfig fills cfg (a pointer to an openaitool config struct) with the
// typed config of an OpenAI provider tool: its ProviderOptions when that
// holds the same type, else the TS args (ProviderArgs) of a TS-shaped
// provider tool, e.g. one restored from a workflow, whose ProviderOptions
// is no longer the typed struct. The config structs' JSON tags are the TS
// arg names, so the args decode directly. It reports whether a config was
// found.
func toolConfig(t types.Tool, cfg interface{}) bool {
	if setTypedConfig(t.ProviderOptions, cfg) {
		return true
	}
	if t.Type != types.ToolTypeProviderDefined || t.ProviderArgs == nil {
		return false
	}
	b, err := json.Marshal(t.ProviderArgs)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, cfg) == nil
}

// setTypedConfig copies options into cfg when options is the config type
// cfg points to (or a non-nil pointer to it).
func setTypedConfig(options interface{}, cfg interface{}) bool {
	switch dst := cfg.(type) {
	case *openaitool.CodeInterpreterConfig:
		switch v := options.(type) {
		case openaitool.CodeInterpreterConfig:
			*dst = v
			return true
		case *openaitool.CodeInterpreterConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.FileSearchConfig:
		switch v := options.(type) {
		case openaitool.FileSearchConfig:
			*dst = v
			return true
		case *openaitool.FileSearchConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.ImageGenerationConfig:
		switch v := options.(type) {
		case openaitool.ImageGenerationConfig:
			*dst = v
			return true
		case *openaitool.ImageGenerationConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.WebSearchConfig:
		switch v := options.(type) {
		case openaitool.WebSearchConfig:
			*dst = v
			return true
		case *openaitool.WebSearchConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.WebSearchPreviewConfig:
		switch v := options.(type) {
		case openaitool.WebSearchPreviewConfig:
			*dst = v
			return true
		case *openaitool.WebSearchPreviewConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.MCPConfig:
		switch v := options.(type) {
		case openaitool.MCPConfig:
			*dst = v
			return true
		case *openaitool.MCPConfig:
			if v != nil {
				*dst = *v
				return true
			}
		}
	case *openaitool.CustomTool:
		if v, ok := options.(openaitool.CustomTool); ok {
			*dst = v
			return true
		}
	}
	return false
}

// openAICustomToolConfig returns the config of an OpenAI custom tool: an
// openaitool.CustomTool in ProviderOptions (CustomTool.ToTool), or the TS
// args of a TS-shaped "openai.custom" provider tool.
func openAICustomToolConfig(t types.Tool) (openaitool.CustomTool, bool) {
	if ct, ok := t.ProviderOptions.(openaitool.CustomTool); ok {
		return ct, true
	}
	if t.Type != types.ToolTypeProviderDefined || t.ProviderID != "openai.custom" {
		return openaitool.CustomTool{}, false
	}
	var ct openaitool.CustomTool
	toolConfig(t, &ct)
	return ct, true
}

// isOpenAICustomTool reports whether t is an OpenAI custom tool.
func isOpenAICustomTool(t types.Tool) bool {
	_, ok := openAICustomToolConfig(t)
	return ok
}

// shellEnvironmentArgs renders a shell environment as the TS openai.shell
// `environment` arg (camelCase, TS environment type names), the inverse of
// TS mapShellEnvironment.
func shellEnvironmentArgs(env ShellEnvironment) map[string]interface{} {
	switch env.Type {
	case "container_reference":
		out := map[string]interface{}{"type": "containerReference"}
		if env.ContainerID != nil {
			out["containerId"] = *env.ContainerID
		}
		return out
	case "container_auto":
		out := map[string]interface{}{"type": "containerAuto"}
		if env.FileIDs != nil {
			out["fileIds"] = env.FileIDs
		}
		if env.MemoryLimit != nil {
			out["memoryLimit"] = *env.MemoryLimit
		}
		if env.NetworkPolicy != nil {
			policy := map[string]interface{}{"type": env.NetworkPolicy.Type}
			if env.NetworkPolicy.AllowedDomains != nil {
				policy["allowedDomains"] = env.NetworkPolicy.AllowedDomains
			}
			if env.NetworkPolicy.DomainSecrets != nil {
				secrets := make([]interface{}, 0, len(env.NetworkPolicy.DomainSecrets))
				for _, s := range env.NetworkPolicy.DomainSecrets {
					secrets = append(secrets, map[string]interface{}{"domain": s.Domain, "name": s.Name, "value": s.Value})
				}
				policy["domainSecrets"] = secrets
			}
			out["networkPolicy"] = policy
		}
		if env.Skills != nil {
			skills := make([]interface{}, 0, len(env.Skills))
			for _, skill := range env.Skills {
				skills = append(skills, shellSkillArgs(skill))
			}
			out["skills"] = skills
		}
		return out
	}
	out := map[string]interface{}{"type": "local"}
	if env.Skills != nil {
		skills := make([]interface{}, 0, len(env.Skills))
		for _, skill := range env.Skills {
			s := map[string]interface{}{}
			setStr(s, "name", skill.Name)
			setStr(s, "description", skill.Description)
			setStr(s, "path", skill.Path)
			skills = append(skills, s)
		}
		out["skills"] = skills
	}
	return out
}

// shellSkillArgs renders a container_auto skill as the TS skill arg: a
// skill_reference becomes {type: "skillReference", providerReference:
// {openai: skillId}, version?}; an inline skill keeps its fields with a
// camelCase source.
func shellSkillArgs(skill ShellSkill) map[string]interface{} {
	s := map[string]interface{}{}
	if skill.Type != nil && *skill.Type == "skill_reference" {
		s["type"] = "skillReference"
		ref := map[string]interface{}{}
		if skill.SkillID != nil {
			ref["openai"] = *skill.SkillID
		}
		s["providerReference"] = ref
		setStr(s, "version", skill.Version)
		return s
	}
	if skill.Type != nil {
		s["type"] = *skill.Type
	}
	setStr(s, "name", skill.Name)
	setStr(s, "description", skill.Description)
	if skill.Source != nil {
		s["source"] = map[string]interface{}{"type": skill.Source.Type, "mediaType": skill.Source.MediaType, "data": skill.Source.Data}
	}
	return s
}

func setStr(m map[string]interface{}, key string, v *string) {
	if v != nil {
		m[key] = *v
	}
}

// providerDefinedTool fills the provider-defined tool identity TS
// createProvider*ToolFactory sets: type "provider", the TS id and the TS
// args. Name stays the TS id, which the direct OpenAI provider dispatches on.
func providerDefinedTool(t types.Tool, id string, args map[string]interface{}) types.Tool {
	t.Type = types.ToolTypeProviderDefined
	t.ProviderID = id
	if args == nil {
		args = map[string]interface{}{}
	}
	t.ProviderArgs = args
	return t
}

// schemaFromJSON decodes a JSON Schema literal into a fresh map.
func schemaFromJSON(s string) map[string]interface{} {
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		panic("openai responses: invalid schema literal: " + err.Error())
	}
	return out
}

// The input and output schemas below are the TS tool schemas (packages/openai/src/tool/{local-shell,shell,apply-patch,computer,programmatic-tool-calling}.ts at e64ed82f27) as zod
// toJSONSchema renders them (target draft-7, io "input", reused "inline").
// Tool calls are validated against the input schema and ValidateUIMessages
// checks tool outputs against the output schema.
const (
	localShellInputSchemaJSON = `{"type":"object","properties":{"action":{"type":"object","properties":{"type":{"type":"string","const":"exec"},"command":{"type":"array","items":{"type":"string"}},"timeoutMs":{"type":"number"},"user":{"type":"string"},"workingDirectory":{"type":"string"},"env":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}}},"required":["type","command"]}},"required":["action"]}`

	localShellOutputSchemaJSON = `{"type":"object","properties":{"output":{"type":"string"}},"required":["output"]}`

	shellInputSchemaJSON = `{"type":"object","properties":{"action":{"type":"object","properties":{"commands":{"type":"array","items":{"type":"string"}},"timeoutMs":{"type":"number"},"maxOutputLength":{"type":"number"}},"required":["commands"]}},"required":["action"]}`

	shellOutputSchemaJSON = `{"type":"object","properties":{"maxOutputLength":{"type":"number"},"output":{"type":"array","items":{"type":"object","properties":{"stdout":{"type":"string"},"stderr":{"type":"string"},"outcome":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"timeout"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"exit"},"exitCode":{"type":"number"}},"required":["type","exitCode"]}]}},"required":["stdout","stderr","outcome"]}}},"required":["output"]}`

	applyPatchInputSchemaJSON = `{"type":"object","properties":{"callId":{"type":"string"},"operation":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"create_file"},"path":{"type":"string"},"diff":{"type":"string"}},"required":["type","path","diff"]},{"type":"object","properties":{"type":{"type":"string","const":"delete_file"},"path":{"type":"string"}},"required":["type","path"]},{"type":"object","properties":{"type":{"type":"string","const":"update_file"},"path":{"type":"string"},"diff":{"type":"string"}},"required":["type","path","diff"]}]}},"required":["callId","operation"]}`

	applyPatchOutputSchemaJSON = `{"type":"object","properties":{"status":{"type":"string","enum":["completed","failed"]},"output":{"type":"string"}},"required":["status"]}`

	computerInputSchemaJSON = `{"type":"object","properties":{"actions":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"click"},"button":{"type":"string","enum":["left","right","wheel","back","forward"]},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","button","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"double_click"},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"drag"},"path":{"type":"array","items":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","path"]},{"type":"object","properties":{"type":{"type":"string","const":"keypress"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","keys"]},{"type":"object","properties":{"type":{"type":"string","const":"move"},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"screenshot"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"scroll"},"x":{"type":"number"},"y":{"type":"number"},"scrollX":{"type":"number"},"scrollY":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y","scrollX","scrollY"]},{"type":"object","properties":{"type":{"type":"string","const":"type"},"text":{"type":"string"}},"required":["type","text"]},{"type":"object","properties":{"type":{"type":"string","const":"wait"}},"required":["type"]}]}},"pendingSafetyChecks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"code":{"type":"string"},"message":{"type":"string"}},"required":["id"]}},"status":{"type":"string","enum":["in_progress","completed","incomplete"]}},"required":["actions","pendingSafetyChecks","status"]}`

	computerOutputSchemaJSON = `{"type":"object","properties":{"output":{"anyOf":[{"type":"object","properties":{"type":{"type":"string","const":"computer_screenshot"},"imageUrl":{"type":"string"},"fileId":{"type":"string"},"detail":{"type":"string","enum":["auto","low","high","original"]}},"required":["type","imageUrl"]},{"type":"object","properties":{"type":{"type":"string","const":"computer_screenshot"},"fileId":{"type":"string"},"imageUrl":{"type":"string"},"detail":{"type":"string","enum":["auto","low","high","original"]}},"required":["type","fileId"]}]},"acknowledgedSafetyChecks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"code":{"type":"string"},"message":{"type":"string"}},"required":["id"]}}},"required":["output"]}`

	programmaticToolCallingInputSchemaJSON = `{"type":"object","properties":{"code":{"type":"string"},"fingerprint":{"type":"string"}},"required":["code","fingerprint"]}`

	programmaticToolCallingOutputSchemaJSON = `{"type":"object","properties":{"result":{"type":"string"},"status":{"type":"string","enum":["completed","incomplete"]}},"required":["result","status"]}`
)

// shellEnvironmentFromArgs maps the TS openai.shell `environment` arg of a
// TS-shaped shell tool to the wire environment, as TS mapShellEnvironment
// does. It returns nil when the args carry no environment, and TS's
// NoSuchProviderReferenceError when a skillReference has no openai entry in
// its providerReference (TS resolveProviderReference).
func shellEnvironmentFromArgs(args map[string]interface{}) (*ShellEnvironment, error) {
	envArgs, ok := args["environment"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	str := func(m map[string]interface{}, key string) *string {
		if s, ok := m[key].(string); ok {
			return &s
		}
		return nil
	}
	strs := func(v interface{}) []string {
		list, ok := v.([]interface{})
		if !ok {
			if s, ok := v.([]string); ok {
				return s
			}
			return nil
		}
		out := make([]string, 0, len(list))
		for _, e := range list {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	maps := func(v interface{}) []map[string]interface{} {
		list, _ := v.([]interface{})
		out := make([]map[string]interface{}, 0, len(list))
		for _, e := range list {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	envType, _ := envArgs["type"].(string)
	switch envType {
	case "containerReference":
		return &ShellEnvironment{Type: "container_reference", ContainerID: str(envArgs, "containerId")}, nil
	case "containerAuto":
		env := &ShellEnvironment{Type: "container_auto", FileIDs: strs(envArgs["fileIds"]), MemoryLimit: str(envArgs, "memoryLimit")}
		if policy, ok := envArgs["networkPolicy"].(map[string]interface{}); ok && policy["type"] == "disabled" {
			// TS: {type: 'disabled'} only.
			env.NetworkPolicy = &ShellNetworkPolicy{Type: "disabled"}
		} else if ok {
			// TS: any other policy is sent as an allowlist.
			p := &ShellNetworkPolicy{Type: "allowlist", AllowedDomains: strs(policy["allowedDomains"])}
			for _, s := range maps(policy["domainSecrets"]) {
				secret := ShellDomainSecret{}
				secret.Domain, _ = s["domain"].(string)
				secret.Name, _ = s["name"].(string)
				secret.Value, _ = s["value"].(string)
				p.DomainSecrets = append(p.DomainSecrets, secret)
			}
			env.NetworkPolicy = p
		}
		for _, s := range maps(envArgs["skills"]) {
			if s["type"] == "skillReference" {
				ref := types.ProviderReference{}
				if raw, ok := s["providerReference"].(map[string]interface{}); ok {
					for k, v := range raw {
						if id, ok := v.(string); ok {
							ref[k] = id
						}
					}
				}
				skillID, err := providerutils.ResolveProviderReference(ref, "openai")
				if err != nil {
					return nil, err
				}
				skillType := "skill_reference"
				// TS: version ?? 'latest' (convertShellTool defaults it).
				env.Skills = append(env.Skills, ShellSkill{Type: &skillType, SkillID: &skillID, Version: str(s, "version")})
				continue
			}
			skillType := "inline"
			skill := ShellSkill{Type: &skillType, Name: str(s, "name"), Description: str(s, "description")}
			if src, ok := s["source"].(map[string]interface{}); ok {
				source := &ShellSkillSource{}
				source.Type, _ = src["type"].(string)
				source.MediaType, _ = src["mediaType"].(string)
				source.Data, _ = src["data"].(string)
				skill.Source = source
			}
			env.Skills = append(env.Skills, skill)
		}
		return env, nil
	}
	env := &ShellEnvironment{Type: "local"}
	for _, s := range maps(envArgs["skills"]) {
		env.Skills = append(env.Skills, ShellSkill{Name: str(s, "name"), Description: str(s, "description"), Path: str(s, "path")})
	}
	return env, nil
}

// tsShapedShellEnvironment returns the environment of a TS-shaped
// openai.shell tool (no typed ShellEnvironment in ProviderOptions), mapped
// from its args; nil for other tools.
func tsShapedShellEnvironment(t types.Tool) (*ShellEnvironment, error) {
	if t.Type != types.ToolTypeProviderDefined || t.ProviderID != "openai.shell" {
		return nil, nil
	}
	switch t.ProviderOptions.(type) {
	case ShellEnvironment, *ShellEnvironment:
		return nil, nil
	}
	return shellEnvironmentFromArgs(t.ProviderArgs)
}
