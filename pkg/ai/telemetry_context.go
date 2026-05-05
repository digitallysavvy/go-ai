package ai

import "reflect"

func telemetryRuntimeContext(settings *TelemetrySettings, contextValue interface{}) map[string]interface{} {
	return filterIncludedContext(contextValue, settingsIncludeRuntime(settings))
}

func telemetryToolsContext(settings *TelemetrySettings, toolsContext map[string]interface{}) map[string]interface{} {
	if settings == nil || len(settings.IncludeToolsContext) == 0 || len(toolsContext) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(toolsContext))
	for toolName, toolContext := range toolsContext {
		filtered := filterIncludedContext(toolContext, settings.IncludeToolsContext[toolName])
		if len(filtered) > 0 {
			out[toolName] = filtered
		}
	}
	return out
}

func telemetryToolContext(settings *TelemetrySettings, toolName string, toolContext interface{}) map[string]interface{} {
	if settings == nil {
		return map[string]interface{}{}
	}
	return filterIncludedContext(toolContext, settings.IncludeToolsContext[toolName])
}

func settingsIncludeRuntime(settings *TelemetrySettings) map[string]bool {
	if settings == nil {
		return nil
	}
	return settings.IncludeRuntimeContext
}

func filterIncludedContext(contextValue interface{}, include map[string]bool) map[string]interface{} {
	if len(include) == 0 || contextValue == nil {
		return map[string]interface{}{}
	}
	source := contextAsMap(contextValue)
	if len(source) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{})
	for key, enabled := range include {
		if !enabled {
			continue
		}
		if value, ok := source[key]; ok {
			out[key] = value
		}
	}
	return out
}

func contextAsMap(contextValue interface{}) map[string]interface{} {
	if value, ok := contextValue.(map[string]interface{}); ok {
		return value
	}
	rv := reflect.ValueOf(contextValue)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	out := make(map[string]interface{}, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out
}
