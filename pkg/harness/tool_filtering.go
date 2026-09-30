package harness

import (
	"fmt"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ResolvedToolFiltering is the result of ResolveToolFiltering. Mirrors TS
// `ResolvedHarnessAgentToolFiltering`.
type ResolvedToolFiltering struct {
	// ActiveUserTools is the (possibly narrowed) set of host-executed tools.
	ActiveUserTools map[string]types.Tool
	// BuiltinToolFiltering is nil when every built-in tool stays available.
	BuiltinToolFiltering *BuiltinToolFiltering
}

// ResolveToolFilteringOptions is the input of ResolveToolFiltering.
type ResolveToolFilteringOptions struct {
	Harness       Harness
	UserTools     map[string]types.Tool
	AllTools      map[string]types.Tool
	ActiveTools   []string // nil when unset
	InactiveTools []string // nil when unset
}

// ResolveToolFiltering computes the active user tool set and the built-in tool
// filtering to forward to the adapter, from `activeTools` / `inactiveTools`.
// Mirrors TS `resolveHarnessAgentToolFiltering`. Returns an error identical in
// meaning to TS's `NoSuchToolError` / plain `Error` throws.
func ResolveToolFiltering(opts ResolveToolFilteringOptions) (ResolvedToolFiltering, error) {
	if opts.ActiveTools != nil && opts.InactiveTools != nil {
		return ResolvedToolFiltering{}, fmt.Errorf("HarnessAgent: pass either `activeTools` or `inactiveTools`, not both.")
	}

	allToolNames := make([]string, 0, len(opts.AllTools))
	for name := range opts.AllTools {
		allToolNames = append(allToolNames, name)
	}

	activeTools := dedupeToolNames(opts.ActiveTools)
	inactiveTools := dedupeToolNames(opts.InactiveTools)

	requested := activeTools
	if requested == nil {
		requested = inactiveTools
	}
	if err := validateToolNames(requested, allToolNames); err != nil {
		return ResolvedToolFiltering{}, err
	}

	userToolNames := make([]string, 0, len(opts.UserTools))
	for name := range opts.UserTools {
		userToolNames = append(userToolNames, name)
	}

	var activeUserToolNames []string
	switch {
	case activeTools != nil:
		activeUserToolNames = filterNames(userToolNames, activeTools, true)
	case inactiveTools != nil:
		activeUserToolNames = filterNames(userToolNames, inactiveTools, false)
	default:
		activeUserToolNames = userToolNames
	}

	builtinToolNames := make([]string, 0, len(opts.Harness.BuiltinTools()))
	for name := range opts.Harness.BuiltinTools() {
		builtinToolNames = append(builtinToolNames, name)
	}

	var disabledBuiltinToolNames []string
	switch {
	case activeTools != nil:
		disabledBuiltinToolNames = filterNames(builtinToolNames, activeTools, false)
	case inactiveTools != nil:
		disabledBuiltinToolNames = filterNames(builtinToolNames, inactiveTools, true)
	}

	var builtinToolFiltering *BuiltinToolFiltering
	if len(disabledBuiltinToolNames) > 0 {
		if activeTools != nil {
			builtinToolFiltering = &BuiltinToolFiltering{
				Mode:      BuiltinToolFilteringAllow,
				ToolNames: filterNames(builtinToolNames, activeTools, true),
			}
		} else {
			builtinToolFiltering = &BuiltinToolFiltering{
				Mode:      BuiltinToolFilteringDeny,
				ToolNames: disabledBuiltinToolNames,
			}
		}
	}

	if builtinToolFiltering != nil &&
		!SupportsBuiltinToolFiltering(opts.Harness) &&
		!SupportsBuiltinToolApprovals(opts.Harness) {
		return ResolvedToolFiltering{}, NewCapabilityUnsupportedError(
			fmt.Sprintf("Harness '%s' does not support built-in tool filtering controls.", opts.Harness.HarnessID()),
			opts.Harness.HarnessID(),
			nil,
		)
	}

	return ResolvedToolFiltering{
		ActiveUserTools:      filterToolSet(opts.UserTools, activeUserToolNames),
		BuiltinToolFiltering: builtinToolFiltering,
	}, nil
}

func dedupeToolNames(names []string) []string {
	if names == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func validateToolNames(requested, available []string) error {
	if requested == nil {
		return nil
	}
	availableSet := map[string]struct{}{}
	for _, n := range available {
		availableSet[n] = struct{}{}
	}
	for _, name := range requested {
		if _, ok := availableSet[name]; !ok {
			names := append([]string(nil), available...)
			sort.Strings(names)
			return &ai.NoSuchToolError{ToolName: name, AvailableTools: names}
		}
	}
	return nil
}

// filterNames returns the subset of names that is (include=true) or is not
// (include=false) present in filter.
func filterNames(names, filter []string, include bool) []string {
	set := map[string]struct{}{}
	for _, n := range filter {
		set[n] = struct{}{}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		_, in := set[n]
		if in == include {
			out = append(out, n)
		}
	}
	return out
}

func filterToolSet(tools map[string]types.Tool, names []string) map[string]types.Tool {
	allowed := map[string]struct{}{}
	for _, n := range names {
		allowed[n] = struct{}{}
	}
	out := make(map[string]types.Tool, len(allowed))
	for name, tool := range tools {
		if _, ok := allowed[name]; ok {
			out[name] = tool
		}
	}
	return out
}
