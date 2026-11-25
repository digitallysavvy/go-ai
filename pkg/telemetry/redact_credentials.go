package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const redactedToolValue = "[redacted]"

// redactCredentials returns a telemetry-only copy of event with known
// provider credentials replaced by "[redacted]" (TS
// redact-telemetry-credentials.ts, 404ce3b14d / 9f37b45262). FilterEvent
// applies it to every event before it reaches an integration or the
// diagnostic channel; the provider request and the values returned to the
// application keep the real credentials.
//
// Only SDK metadata containers are visited, as in TS, never prompts, tool
// inputs or outputs, or user context:
//   - Tools / StepTools: each provider tool's ProviderArgs
//     (redactProviderToolArgs);
//   - Request / Response: the Body (redactBody);
//   - Steps / FinalStep: the same, per step.
//
// TS also visits error / cause / errors / lastError for the
// requestBodyValues of an APICallError; no Go error carries a request body,
// so there is nothing to redact there.
//
// Values are copied only along the changed path; event itself is never
// modified.
func redactCredentials[E any](event E) E {
	v := reflect.ValueOf(&event).Elem()
	if out, ok := redactAnyValue(v); ok {
		v.Set(out)
	}
	return event
}

// redactAnyValue redacts a struct, a pointer to a struct or an interface
// holding either. ok is false when nothing changed (v is then unchanged).
func redactAnyValue(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Struct:
		return redactStructValue(v)
	case reflect.Pointer:
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return v, false
		}
		inner, ok := redactStructValue(v.Elem())
		if !ok {
			return v, false
		}
		cp := reflect.New(v.Elem().Type())
		cp.Elem().Set(inner)
		return cp, true
	case reflect.Interface:
		if v.IsNil() {
			return v, false
		}
		inner, ok := redactAnyValue(v.Elem())
		if !ok {
			return v, false
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out, true
	}
	return v, false
}

func redactStructValue(s reflect.Value) (reflect.Value, bool) {
	type replacement struct {
		index int
		value reflect.Value
	}
	var changes []replacement
	t := s.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		f := s.Field(i)
		var out reflect.Value
		var ok bool
		switch sf.Name {
		case "Tools", "StepTools":
			out, ok = redactToolsValue(f)
		case "Request", "Response":
			out, ok = redactMetadataValue(f)
		case "Steps":
			out, ok = redactStepsValue(f)
		case "FinalStep":
			out, ok = redactAnyValue(f)
		default:
			if sf.Type == errorInterface {
				out, ok = redactErrorValue(f)
			}
		}
		if ok {
			changes = append(changes, replacement{i, out})
		}
	}
	if len(changes) == 0 {
		return s, false
	}
	cp := reflect.New(t).Elem()
	cp.Set(s)
	for _, c := range changes {
		cp.Field(c.index).Set(c.value)
	}
	return cp, true
}

func redactStepsValue(f reflect.Value) (reflect.Value, bool) {
	if f.Kind() != reflect.Slice || f.IsNil() {
		return f, false
	}
	var out reflect.Value
	for i := 0; i < f.Len(); i++ {
		step, ok := redactAnyValue(f.Index(i))
		if !ok {
			continue
		}
		if !out.IsValid() {
			out = reflect.MakeSlice(f.Type(), f.Len(), f.Len())
			reflect.Copy(out, f)
		}
		out.Index(i).Set(step)
	}
	if !out.IsValid() {
		return f, false
	}
	return out, true
}

// redactMetadataValue redacts the Body of request / response metadata (TS
// redactMetadata): a struct, a pointer to one, an interface holding either,
// or a list of them.
func redactMetadataValue(f reflect.Value) (reflect.Value, bool) {
	switch f.Kind() {
	case reflect.Slice:
		if f.IsNil() {
			return f, false
		}
		var out reflect.Value
		for i := 0; i < f.Len(); i++ {
			item, ok := redactMetadataValue(f.Index(i))
			if !ok {
				continue
			}
			if !out.IsValid() {
				out = reflect.MakeSlice(f.Type(), f.Len(), f.Len())
				reflect.Copy(out, f)
			}
			out.Index(i).Set(item)
		}
		if !out.IsValid() {
			return f, false
		}
		return out, true
	case reflect.Struct:
		body := f.FieldByName("Body")
		if !body.IsValid() || body.Kind() != reflect.Interface || body.IsNil() {
			return f, false
		}
		redacted, ok := redactBody(body.Interface())
		if !ok {
			return f, false
		}
		cp := reflect.New(f.Type()).Elem()
		cp.Set(f)
		bf := cp.FieldByName("Body")
		if !bf.CanSet() {
			return f, false
		}
		if redacted == nil {
			bf.Set(reflect.Zero(bf.Type()))
		} else {
			bf.Set(reflect.ValueOf(redacted))
		}
		return cp, true
	case reflect.Pointer:
		if f.IsNil() || f.Elem().Kind() != reflect.Struct {
			return f, false
		}
		inner, ok := redactMetadataValue(f.Elem())
		if !ok {
			return f, false
		}
		cp := reflect.New(f.Elem().Type())
		cp.Elem().Set(inner)
		return cp, true
	case reflect.Interface:
		if f.IsNil() {
			return f, false
		}
		inner, ok := redactMetadataValue(f.Elem())
		if !ok {
			return f, false
		}
		out := reflect.New(f.Type()).Elem()
		out.Set(inner)
		return out, true
	}
	return f, false
}

// redactToolsValue redacts the provider tools of a Tools / StepTools field
// (TS redactTools): a list or name-keyed map of types.Tool (or pointers to
// them), or of JSON tool definitions with `id` and `args`, possibly held in
// an interface.
func redactToolsValue(f reflect.Value) (reflect.Value, bool) {
	if !f.IsValid() || !f.CanInterface() {
		return f, false
	}
	if f.Kind() == reflect.Interface {
		if f.IsNil() {
			return f, false
		}
		inner, ok := redactToolsValue(f.Elem())
		if !ok {
			return f, false
		}
		out := reflect.New(f.Type()).Elem()
		out.Set(inner)
		return out, true
	}
	switch tools := f.Interface().(type) {
	case []types.Tool:
		var out []types.Tool
		for i, t := range tools {
			if r, ok := redactTool(t); ok {
				if out == nil {
					out = append([]types.Tool(nil), tools...)
				}
				out[i] = r
			}
		}
		if out == nil {
			return f, false
		}
		return reflect.ValueOf(out).Convert(f.Type()), true
	case []*types.Tool:
		var out []*types.Tool
		for i, t := range tools {
			if t == nil {
				continue
			}
			if r, ok := redactTool(*t); ok {
				if out == nil {
					out = append([]*types.Tool(nil), tools...)
				}
				out[i] = &r
			}
		}
		if out == nil {
			return f, false
		}
		return reflect.ValueOf(out).Convert(f.Type()), true
	case map[string]types.Tool:
		var out map[string]types.Tool
		for name, t := range tools {
			if r, ok := redactTool(t); ok {
				if out == nil {
					out = make(map[string]types.Tool, len(tools))
					for k, v := range tools {
						out[k] = v
					}
				}
				out[name] = r
			}
		}
		if out == nil {
			return f, false
		}
		return reflect.ValueOf(out).Convert(f.Type()), true
	case map[string]*types.Tool:
		var out map[string]*types.Tool
		for name, t := range tools {
			if t == nil {
				continue
			}
			if r, ok := redactTool(*t); ok {
				if out == nil {
					out = make(map[string]*types.Tool, len(tools))
					for k, v := range tools {
						out[k] = v
					}
				}
				out[name] = &r
			}
		}
		if out == nil {
			return f, false
		}
		return reflect.ValueOf(out).Convert(f.Type()), true
	case []*jsonorder.Object:
		// Prepared tool definitions (toolDefinitionsList takes them as
		// given): keep the type and each definition's key order.
		var out []*jsonorder.Object
		for i, def := range tools {
			if r, ok := redactOrderedToolDefinition(def); ok {
				if out == nil {
					out = append([]*jsonorder.Object(nil), tools...)
				}
				out[i] = r
			}
		}
		if out == nil {
			return f, false
		}
		return reflect.ValueOf(out).Convert(f.Type()), true
	}
	if list, ok := asToolList(f.Interface()); ok {
		out, changed := redactToolDefinitions(list, false)
		if changed && reflect.TypeOf(out).AssignableTo(f.Type()) {
			return reflect.ValueOf(out), true
		}
	}
	return f, false
}

// redactOrderedToolDefinition redacts the args of a prepared provider tool
// definition ({type, name, id, args}) by its id, keeping the key order.
func redactOrderedToolDefinition(def *jsonorder.Object) (*jsonorder.Object, bool) {
	if def == nil {
		return def, false
	}
	idValue, _ := def.Get("id")
	id, _ := idValue.(string)
	argsValue, _ := def.Get("args")
	args, ok := asToolRecord(argsValue)
	if id == "" || !ok {
		return def, false
	}
	redacted, changed := redactProviderToolArgsChanged(id, args)
	if !changed {
		return def, false
	}
	out := jsonorder.New()
	for _, k := range def.Keys() {
		v, _ := def.Get(k)
		out.Set(k, v)
	}
	out.Set("args", redacted)
	return out, true
}

// redactTool redacts a provider tool's ProviderArgs by its provider ID, and
// its ProviderOptions: Go's OpenAI tool constructors keep the typed config
// the request builder reads there (MCPConfig, CodeInterpreterConfig, the
// shell environment), with the same credentials.
func redactTool(t types.Tool) (types.Tool, bool) {
	if t.ProviderID == "" {
		return t, false
	}
	changed := false
	if t.ProviderArgs != nil {
		if args, ok := redactProviderToolArgsChanged(t.ProviderID, t.ProviderArgs); ok {
			t.ProviderArgs, changed = args, true
		}
	}
	if t.ProviderOptions != nil {
		if opts, ok := redactProviderToolOptions(t.ProviderID, t.ProviderOptions); ok {
			t.ProviderOptions, changed = opts, true
		}
	}
	return t, changed
}

// redactProviderToolOptions redacts the typed config of an OpenAI provider
// tool, read through its JSON form (SDK or wire key spellings). A redacted
// config becomes its JSON object form in the telemetry copy.
func redactProviderToolOptions(id string, options interface{}) (interface{}, bool) {
	var kind, containerKey string
	switch id {
	case "openai.mcp":
		kind = "mcp"
	case "openai.code_interpreter":
		kind, containerKey = "code_interpreter", "container"
	case "openai.shell":
		kind, containerKey = "shell", "environment"
	default:
		return options, false
	}
	rec, ok := asToolRecord(options)
	if !ok {
		return options, false
	}
	if kind == "mcp" {
		out, changed := redactOpenAIToolArgs(kind, rec, false)
		return out, changed
	}
	both := func(m map[string]interface{}) (map[string]interface{}, bool) {
		a, c1 := redactOpenAIToolArgs(kind, m, false)
		b, c2 := redactOpenAIToolArgs(kind, a, true)
		return b, c1 || c2
	}
	out, changed := both(rec)
	// The config may be the container / environment itself.
	if wrapped, ok := both(map[string]interface{}{containerKey: out}); ok {
		if inner, isRec := wrapped[containerKey].(map[string]interface{}); isRec {
			out, changed = inner, true
		}
	}
	if !changed {
		return options, false
	}
	return out, true
}

// redactToolDefinitions redacts JSON tool definitions: the `args` of each
// definition with an `id` (the provider tool definitions a gateway forwards,
// TS redactTools on a body), and, when wire is true, the OpenAI Responses
// wire tools by their `type` (TS withOpenAIResponseRedaction). The result is
// a new []interface{} when anything changed.
func redactToolDefinitions(list []interface{}, wire bool) ([]interface{}, bool) {
	var out []interface{}
	for i, item := range list {
		rec, ok := asToolRecord(item)
		if !ok {
			continue
		}
		next, changed := rec, false
		if wire {
			if kind, _ := rec["type"].(string); kind == "mcp_server" {
				// Google Interactions wire tool (TS b0fccbda62).
				next, changed = redactGoogleMCPServerArgs(next)
			} else if kind != "" {
				next, changed = redactOpenAIToolArgs(kind, next, true)
			}
		}
		if id, _ := next["id"].(string); id != "" {
			if args, ok := asToolRecord(next["args"]); ok {
				if redacted, argsChanged := redactProviderToolArgsChanged(id, args); argsChanged {
					if !changed {
						next = copyRecord(next)
					}
					next["args"] = redacted
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		if out == nil {
			out = append([]interface{}(nil), list...)
		}
		out[i] = next
	}
	if out == nil {
		return list, false
	}
	return out, true
}

// redactBody redacts a raw request or response body (TS redactBody): the
// OpenAI Responses tool credentials in its `tools` (wire keys), then the
// provider tool definitions a gateway forwards there. A body that is JSON
// text stays JSON text of the same Go type. ok is false when nothing
// changed.
//
// TS applies the wire rules only to bodies the OpenAI Responses model
// annotated; Go has no annotation, so it applies them to every body with
// such a tool (an MCP tool's authorization in an xAI Responses body is
// redacted too: known difference, S2).
func redactBody(body interface{}) (interface{}, bool) {
	switch b := body.(type) {
	case nil:
		return body, false
	case string:
		out, ok := redactJSONTextBody([]byte(b))
		if !ok {
			return body, false
		}
		return string(out), true
	case json.RawMessage:
		out, ok := redactJSONTextBody(b)
		if !ok {
			return body, false
		}
		return json.RawMessage(out), true
	case []byte:
		out, ok := redactJSONTextBody(b)
		if !ok {
			return body, false
		}
		return out, true
	}
	if isNilValue(body) {
		return body, false
	}
	switch reflect.Indirect(reflect.ValueOf(body)).Kind() {
	case reflect.Map, reflect.Struct:
	default:
		return body, false
	}
	rec, ok := asToolRecord(body)
	if !ok {
		return body, false
	}
	out, changed := rec, false
	if tools, ok := asToolList(rec["tools"]); ok {
		if redacted, toolsChanged := redactToolDefinitions(tools, true); toolsChanged {
			out, changed = copyRecord(rec), true
			out["tools"] = redacted
		}
	}
	if next, byokChanged := redactGatewayByok(out); byokChanged {
		out, changed = next, true
	}
	if !changed {
		return body, false
	}
	return out, true
}

// redactGatewayByok is TS withGatewayRequestTelemetryRedaction
// (redact-gateway-request-credentials.ts, 83026e8e0f): in a Gateway request
// body every value under providerOptions.gateway.byok, and under each batch
// request's options.providerOptions.gateway.byok, becomes "[redacted]" while
// provider names and the credential shape (objects, lists) stay. rec is not
// modified; the result is a copy when something changed. TS annotates only
// Gateway bodies; Go has no annotation, so any body with that key is
// redacted (known difference S2, as for the OpenAI wire rule).
func redactGatewayByok(rec map[string]interface{}) (map[string]interface{}, bool) {
	out, changed := rec, false
	if po, ok := redactProviderOptionsByok(rec["providerOptions"]); ok {
		out, changed = copyRecord(rec), true
		out["providerOptions"] = po
	}
	if requests, ok := asToolList(rec["requests"]); ok {
		var redacted []interface{}
		for i, item := range requests {
			request, ok := asToolRecord(item)
			if !ok {
				continue
			}
			options, ok := asToolRecord(request["options"])
			if !ok {
				continue
			}
			po, ok := redactProviderOptionsByok(options["providerOptions"])
			if !ok {
				continue
			}
			if redacted == nil {
				redacted = append([]interface{}(nil), requests...)
			}
			newOptions := copyRecord(options)
			newOptions["providerOptions"] = po
			newRequest := copyRecord(request)
			newRequest["options"] = newOptions
			redacted[i] = newRequest
		}
		if redacted != nil {
			if !changed {
				out, changed = copyRecord(rec), true
			}
			out["requests"] = redacted
		}
	}
	return out, changed
}

func redactProviderOptionsByok(value interface{}) (map[string]interface{}, bool) {
	po, ok := asToolRecord(value)
	if !ok {
		return nil, false
	}
	gateway, ok := asToolRecord(po["gateway"])
	if !ok {
		return nil, false
	}
	byok, present := gateway["byok"]
	if !present {
		return nil, false
	}
	newGateway := copyRecord(gateway)
	newGateway["byok"] = redactByokValue(byok)
	out := copyRecord(po)
	out["gateway"] = newGateway
	return out, true
}

// redactByokValue keeps objects and lists and replaces every leaf.
func redactByokValue(v interface{}) interface{} {
	if list, ok := asToolList(v); ok {
		out := make([]interface{}, len(list))
		for i, item := range list {
			out[i] = redactByokValue(item)
		}
		return out
	}
	if rec, ok := asToolRecord(v); ok {
		out := make(map[string]interface{}, len(rec))
		for k, item := range rec {
			out[k] = redactByokValue(item)
		}
		return out
	}
	return redactedToolValue
}

func redactJSONTextBody(raw []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || (!bytes.Contains(trimmed, []byte(`"tools"`)) && !bytes.Contains(trimmed, []byte(`"byok"`))) {
		return raw, false
	}
	var decoded map[string]interface{}
	if json.Unmarshal(trimmed, &decoded) != nil {
		return raw, false
	}
	redacted, ok := redactBody(decoded)
	if !ok {
		return raw, false
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		// Never fall back to the unredacted text.
		return []byte("{}"), true
	}
	return out, true
}

// redactProviderToolArgs removes provider credentials from a telemetry-only
// copy of a provider tool's args (TS redactOpenAIToolArgs, the redactArgs of
// the openai.mcp, openai.code_interpreter and openai.shell factories). args
// itself is never modified (the provider request needs the real values);
// other tools pass through unchanged.
func redactProviderToolArgs(id string, args map[string]interface{}) map[string]interface{} {
	out, _ := redactProviderToolArgsChanged(id, args)
	return out
}

func redactProviderToolArgsChanged(id string, args map[string]interface{}) (map[string]interface{}, bool) {
	switch id {
	case "openai.mcp":
		return redactOpenAIToolArgs("mcp", args, false)
	case "openai.code_interpreter":
		return redactOpenAIToolArgs("code_interpreter", args, false)
	case "openai.shell":
		return redactOpenAIToolArgs("shell", args, false)
	case "google.mcp_server":
		return redactGoogleMCPServerArgs(args)
	}
	return args, false
}

// redactGoogleMCPServerArgs is TS redactToolDefinition's google.mcp_server
// branch and redactGoogleToolArgs (b0fccbda62): every header value of a
// Google Interactions MCP server tool becomes "[redacted]" (header names are
// kept); args without a headers record pass through unchanged. args itself is
// never modified.
func redactGoogleMCPServerArgs(args map[string]interface{}) (map[string]interface{}, bool) {
	headers, ok := asToolRecord(args["headers"])
	if !ok {
		return args, false
	}
	redacted := make(map[string]interface{}, len(headers))
	for name := range headers {
		redacted[name] = redactedToolValue
	}
	out := copyRecord(args)
	out["headers"] = redacted
	return out, true
}

// redactOpenAIToolArgs is TS redactOpenAIToolArgs
// (redact-openai-tool-credentials.ts): for an MCP tool the authorization and
// every header value; for a code interpreter container or a shell
// environment the value of each network policy domain secret. wire selects
// the Responses API keys (network_policy / domain_secrets) instead of the
// SDK ones (networkPolicy / domainSecrets).
func redactOpenAIToolArgs(kind string, args map[string]interface{}, wire bool) (map[string]interface{}, bool) {
	if kind == "mcp" {
		var out map[string]interface{}
		if v, ok := args["authorization"]; ok && !isNilValue(v) {
			out = copyRecord(args)
			out["authorization"] = redactedToolValue
		}
		if headers, ok := asToolRecord(args["headers"]); ok {
			if out == nil {
				out = copyRecord(args)
			}
			redacted := make(map[string]interface{}, len(headers))
			for name := range headers {
				redacted[name] = redactedToolValue
			}
			out["headers"] = redacted
		}
		if out == nil {
			return args, false
		}
		return out, true
	}
	var containerKey string
	switch kind {
	case "code_interpreter":
		containerKey = "container"
	case "shell":
		containerKey = "environment"
	default:
		return args, false
	}
	policyKey, secretsKey := "networkPolicy", "domainSecrets"
	if wire {
		policyKey, secretsKey = "network_policy", "domain_secrets"
	}
	container, ok := asToolRecord(args[containerKey])
	if !ok {
		return args, false
	}
	policy, ok := asToolRecord(container[policyKey])
	if !ok {
		return args, false
	}
	secrets, ok := asToolList(policy[secretsKey])
	if !ok {
		return args, false
	}
	redacted := make([]interface{}, len(secrets))
	for i, secret := range secrets {
		if rec, ok := asToolRecord(secret); ok {
			if _, has := rec["value"]; has {
				cp := copyRecord(rec)
				cp["value"] = redactedToolValue
				redacted[i] = cp
				continue
			}
		}
		redacted[i] = secret
	}
	newPolicy := copyRecord(policy)
	newPolicy[secretsKey] = redacted
	newContainer := copyRecord(container)
	newContainer[policyKey] = newPolicy
	out := copyRecord(args)
	out[containerKey] = newContainer
	return out, true
}

// redactErrorValue is TS redactDownloadError (15bfd6b262): an error that is
// or wraps a download error becomes a telemetry-only *DownloadError with the
// URL "[redacted]", the status kept, no cause, and the message "Failed to
// download a file." A file URL can carry credentials (a signed query
// string), and the original message and cause print it. Wrapping errors
// around the download error are dropped with it, since their text repeats
// the URL; errors.As still finds the *DownloadError. Other errors are left
// as they are.
func redactErrorValue(f reflect.Value) (reflect.Value, bool) {
	if f.IsNil() {
		return f, false
	}
	err, ok := f.Interface().(error)
	if !ok {
		return f, false
	}
	var download *providererrors.DownloadError
	if !errors.As(err, &download) || download == nil {
		return f, false
	}
	out := reflect.New(f.Type()).Elem()
	out.Set(reflect.ValueOf(error(redactDownloadError(download))))
	return out, true
}

func redactDownloadError(e *providererrors.DownloadError) *providererrors.DownloadError {
	return &providererrors.DownloadError{
		URL:        redactedToolValue,
		StatusCode: e.StatusCode,
		StatusText: e.StatusText,
		Message:    "Failed to download a file.",
	}
}
