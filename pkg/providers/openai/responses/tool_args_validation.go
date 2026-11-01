package responses

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// validateOpenAIToolArgsSchema mirrors the validateTypes call TS
// prepareResponsesTools makes on each provider tool's args before building
// the request: invalid args (an unknown enum value, an out-of-range
// outputCompression, a grammar format without a definition, ...) fail with
// a type validation error, listing zod's issues, instead of reaching the
// API. Tools without TS args (a hand-built tool with only a typed
// ProviderOptions config) are not checked.
func validateOpenAIToolArgsSchema(t types.Tool) error {
	if t.Type != types.ToolTypeProviderDefined || t.ProviderArgs == nil {
		return nil
	}
	node, ok := openAIToolArgsNodes[t.ProviderID]
	if !ok {
		return nil
	}
	b, err := json.Marshal(t.ProviderArgs)
	if err != nil {
		return openAIToolArgsError(t.ProviderArgs, err.Error(), err)
	}
	var args interface{}
	if err := json.Unmarshal(b, &args); err != nil {
		return openAIToolArgsError(t.ProviderArgs, err.Error(), err)
	}
	if issues := zodIssueText(node, args); issues != "" {
		// TS: the cause is the ZodError, whose message is the issue list.
		return openAIToolArgsError(args, issues, errors.New(issues))
	}
	return nil
}

// zodIssueText validates args against node and returns zod's ZodError
// message (JSON.stringify(issues, null, 2)), or "" when args match.
func zodIssueText(node zodlite.Node, args interface{}) string {
	data, err := jsonstr.Marshal(args)
	if err != nil {
		return ""
	}
	return zodlite.IssueText(zodlite.ValidationMessage(node, string(data)))
}

// openAIToolArgsError is the TypeValidationError TS validateTypes throws
// without a context: "Type validation failed: Value: <JSON.stringify(args)>.
// \nError message: <issues>" (zod's issue list).
func openAIToolArgsError(args interface{}, issues string, cause error) error {
	value := ""
	if b, err := jsonstr.Marshal(args); err == nil {
		value = string(b)
	} else {
		value = fmt.Sprintf("%v", args)
	}
	return &providererrors.TypeValidationError{
		Context: &providererrors.ValidationContext{},
		Value:   value,
		Message: issues,
		Cause:   cause,
	}
}

// openAIToolArgsNodes are the TS args schemas (packages/openai/src/tool/*.ts
// at e64ed82f27, the *ArgsSchema each prepareResponsesTools case validates
// with) as zodlite nodes, so a failure lists zod 4's exact issues.
var openAIToolArgsNodes = func() map[string]zodlite.Node {
	var (
		str      = zodlite.String
		num      = zodlite.Number
		boolean  = zodlite.Boolean
		opt      = zodlite.Optional
		f        = zodlite.F
		obj      = zodlite.Object
		arr      = zodlite.Array
		enum     = zodlite.Enum
		lit      = zodlite.Literal
		strArray = arr(str)
	)
	userLocation := obj(
		f("type", lit("approximate")),
		f("country", opt(str)), f("city", opt(str)),
		f("region", opt(str)), f("timezone", opt(str)),
	)
	searchContextSize := opt(enum("low", "medium", "high"))

	// file-search.ts comparisonFilterSchema / compoundFilterSchema (recursive).
	comparisonFilter := obj(
		f("key", str),
		f("type", enum("eq", "ne", "gt", "gte", "lt", "lte", "in", "nin")),
		f("value", zodlite.Union(str, num, boolean, strArray)),
	)
	var compoundFilter zodlite.Node
	compoundFilter = obj(
		f("type", enum("and", "or")),
		f("filters", arr(zodlite.Union(comparisonFilter, zodlite.Lazy(func() zodlite.Node { return compoundFilter })))),
	)

	// shell.ts shellSkillsSchema and the environment union.
	shellSkills := opt(arr(zodlite.DiscriminatedUnion([]string{"skillReference", "inline"}, map[string]zodlite.Node{
		"skillReference": zodlite.Typed("skillReference",
			f("providerReference", zodlite.RecordOf(str)),
			f("version", opt(str)),
		),
		"inline": zodlite.Typed("inline",
			f("name", str), f("description", str),
			f("source", obj(
				f("type", lit("base64")),
				f("mediaType", lit("application/zip")),
				f("data", str),
			)),
		),
	})))
	shellEnvironment := zodlite.Union(
		zodlite.Typed("containerAuto",
			f("fileIds", opt(strArray)),
			f("memoryLimit", opt(enum("1g", "4g", "16g", "64g"))),
			f("networkPolicy", opt(zodlite.DiscriminatedUnion([]string{"disabled", "allowlist"}, map[string]zodlite.Node{
				"disabled": zodlite.Typed("disabled"),
				"allowlist": zodlite.Typed("allowlist",
					f("allowedDomains", strArray),
					f("domainSecrets", opt(arr(obj(f("domain", str), f("name", str), f("value", str))))),
				),
			}))),
			f("skills", shellSkills),
		),
		zodlite.Typed("containerReference", f("containerId", str)),
		obj(
			f("type", opt(lit("local"))),
			f("skills", opt(arr(obj(f("name", str), f("description", str), f("path", str))))),
		),
	)
	intRange := func(min, max int64) zodlite.Node {
		return opt(zodlite.Max(zodlite.Min(zodlite.Int, min), max))
	}

	return map[string]zodlite.Node{
		"openai.web_search": obj(
			f("externalWebAccess", opt(boolean)),
			f("filters", opt(obj(f("allowedDomains", opt(strArray)), f("blockedDomains", opt(strArray))))),
			f("searchContextSize", searchContextSize),
			f("userLocation", opt(userLocation)),
		),
		"openai.web_search_preview": obj(
			f("searchContextSize", searchContextSize),
			f("userLocation", opt(userLocation)),
		),
		"openai.code_interpreter": obj(
			f("container", opt(zodlite.Union(str, obj(f("fileIds", opt(strArray)))))),
		),
		"openai.file_search": obj(
			f("vectorStoreIds", strArray),
			f("maxNumResults", opt(num)),
			f("ranking", opt(obj(f("ranker", opt(str)), f("scoreThreshold", opt(num))))),
			f("filters", opt(zodlite.Union(comparisonFilter, compoundFilter))),
		),
		"openai.image_generation": zodlite.StrictObject(
			f("action", opt(enum("generate", "edit", "auto"))),
			f("background", opt(enum("auto", "opaque", "transparent"))),
			f("inputFidelity", opt(enum("low", "high"))),
			f("inputImageMask", opt(obj(f("fileId", opt(str)), f("imageUrl", opt(str))))),
			f("model", opt(str)),
			f("moderation", opt(enum("auto", "low"))),
			f("outputCompression", intRange(0, 100)),
			f("outputFormat", opt(enum("png", "jpeg", "webp"))),
			f("partialImages", intRange(0, 3)),
			f("quality", opt(enum("auto", "low", "medium", "high", "xhigh", "max"))),
			f("size", opt(zodlite.Union(
				enum("1024x1024", "1024x1536", "1536x1024", "auto"),
				zodlite.Regex(`^\d+x\d+$`),
			))),
		),
		// The serverUrl/connectorId refine runs in validateMCPToolArgs.
		"openai.mcp": obj(
			f("serverLabel", str),
			f("allowedTools", opt(zodlite.Union(
				strArray,
				obj(f("readOnly", opt(boolean)), f("toolNames", opt(strArray))),
			))),
			f("authorization", opt(str)),
			f("connectorId", opt(str)),
			f("headers", opt(zodlite.RecordOf(str))),
			f("requireApproval", opt(zodlite.Union(
				enum("always", "never"),
				obj(f("never", opt(obj(f("toolNames", opt(strArray)))))),
			))),
			f("serverDescription", opt(str)),
			f("serverUrl", opt(str)),
		),
		"openai.custom": obj(
			f("description", opt(str)),
			f("async", opt(boolean)),
			f("format", opt(zodlite.Union(
				zodlite.Typed("grammar", f("syntax", enum("regex", "lark")), f("definition", str)),
				zodlite.Typed("text"),
			))),
		),
		"openai.tool_search": obj(
			f("execution", opt(enum("server", "client"))),
			f("description", opt(str)),
			f("parameters", opt(zodlite.Record)),
		),
		"openai.shell": obj(
			f("environment", opt(shellEnvironment)),
		),
	}
}()
