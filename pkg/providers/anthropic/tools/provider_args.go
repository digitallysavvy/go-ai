package tools

// The builders in this file produce types.Tool.ProviderArgs for each
// Anthropic provider tool: the camelCase args object the TypeScript SDK's
// provider tool factory holds (tool.args), which a provider-agnostic consumer
// such as the AI Gateway sends as {type: "provider", id, name, args}. Only the
// fields a caller set appear, as TS drops undefined args when it serializes.
// The direct Anthropic provider keeps reading the typed ProviderOptions.

func noProviderArgs() map[string]interface{} { return map[string]interface{}{} }

func webSearchUserLocationArgs(loc *WebSearchUserLocation) map[string]interface{} {
	m := map[string]interface{}{"type": loc.Type}
	if loc.City != "" {
		m["city"] = loc.City
	}
	if loc.Region != "" {
		m["region"] = loc.Region
	}
	if loc.Country != "" {
		m["country"] = loc.Country
	}
	if loc.Timezone != "" {
		m["timezone"] = loc.Timezone
	}
	return m
}

func webCommonArgs(maxUses *int, allowed, blocked []string) map[string]interface{} {
	m := map[string]interface{}{}
	if maxUses != nil {
		m["maxUses"] = *maxUses
	}
	if allowed != nil {
		m["allowedDomains"] = append([]string{}, allowed...)
	}
	if blocked != nil {
		m["blockedDomains"] = append([]string{}, blocked...)
	}
	return m
}

// webSearchProviderArgs is webSearch_20250305/20260209ArgsSchema.
func webSearchProviderArgs(c WebSearch20260209Config) map[string]interface{} {
	m := webCommonArgs(c.MaxUses, c.AllowedDomains, c.BlockedDomains)
	if c.UserLocation != nil {
		m["userLocation"] = webSearchUserLocationArgs(c.UserLocation)
	}
	return m
}

// webSearch20260318ProviderArgs is webSearch_20260318ArgsSchema.
func webSearch20260318ProviderArgs(c WebSearch20260318Config) map[string]interface{} {
	m := webCommonArgs(c.MaxUses, c.AllowedDomains, c.BlockedDomains)
	if c.UserLocation != nil {
		m["userLocation"] = webSearchUserLocationArgs(c.UserLocation)
	}
	if c.ResponseInclusion != "" {
		m["responseInclusion"] = c.ResponseInclusion
	}
	return m
}

func webFetchArgs(maxUses *int, allowed, blocked []string, citations *WebFetchCitations, maxContentTokens *int) map[string]interface{} {
	m := webCommonArgs(maxUses, allowed, blocked)
	if citations != nil {
		m["citations"] = map[string]interface{}{"enabled": citations.Enabled}
	}
	if maxContentTokens != nil {
		m["maxContentTokens"] = *maxContentTokens
	}
	return m
}

// webFetchProviderArgs is webFetch_20250910/20260209ArgsSchema.
func webFetchProviderArgs(c WebFetch20260209Config) map[string]interface{} {
	return webFetchArgs(c.MaxUses, c.AllowedDomains, c.BlockedDomains, c.Citations, c.MaxContentTokens)
}

// webFetch20260318ProviderArgs is webFetch_20260318ArgsSchema.
func webFetch20260318ProviderArgs(c WebFetch20260318Config) map[string]interface{} {
	m := webFetchArgs(c.MaxUses, c.AllowedDomains, c.BlockedDomains, c.Citations, c.MaxContentTokens)
	if c.UseCache != nil {
		m["useCache"] = *c.UseCache
	}
	if c.ResponseInclusion != "" {
		m["responseInclusion"] = c.ResponseInclusion
	}
	return m
}

// computerLegacyProviderArgs is the computer_20241022/20250124 args
// ({displayWidthPx, displayHeightPx, displayNumber?}).
func computerLegacyProviderArgs(a Computer20241022Args) map[string]interface{} {
	m := map[string]interface{}{
		"displayWidthPx":  a.DisplayWidthPx,
		"displayHeightPx": a.DisplayHeightPx,
	}
	if a.DisplayNumber != nil {
		m["displayNumber"] = *a.DisplayNumber
	}
	return m
}

// computer20251124ProviderArgs is the computer_20251124 args
// ({displayWidthPx, displayHeightPx, displayNumber?, enableZoom?}). Go's
// EnableZoom is a plain bool, so only true is sent (false is the default).
func computer20251124ProviderArgs(a Computer20251124Args) map[string]interface{} {
	m := computerLegacyProviderArgs(Computer20241022Args{
		DisplayWidthPx:  a.DisplayWidthPx,
		DisplayHeightPx: a.DisplayHeightPx,
		DisplayNumber:   a.DisplayNumber,
	})
	if a.EnableZoom {
		m["enableZoom"] = true
	}
	return m
}

// computerToolset20260801ProviderArgs is computerToolset_20260801ArgsSchema
// ({configs?: {<member>: {enabled?, deferLoading?}}}).
func computerToolset20260801ProviderArgs(c ComputerToolset20260801Config) map[string]interface{} {
	m := map[string]interface{}{}
	if c.Configs != nil {
		configs := make(map[string]interface{}, len(c.Configs))
		for member, cfg := range c.Configs {
			entry := map[string]interface{}{}
			if cfg.Enabled != nil {
				entry["enabled"] = *cfg.Enabled
			}
			if cfg.DeferLoading != nil {
				entry["deferLoading"] = *cfg.DeferLoading
			}
			configs[string(member)] = entry
		}
		m["configs"] = configs
	}
	return m
}

// textEditor20250728ProviderArgs is textEditor_20250728ArgsSchema.
func textEditor20250728ProviderArgs(a TextEditor20250728Args) map[string]interface{} {
	m := map[string]interface{}{}
	if a.MaxCharacters != nil {
		m["maxCharacters"] = *a.MaxCharacters
	}
	return m
}

// advisor20260301ProviderArgs is advisor_20260301ArgsSchema, after the
// constructor's defaults (model, caching type).
func advisor20260301ProviderArgs(a Advisor20260301Args) map[string]interface{} {
	m := map[string]interface{}{"model": a.Model}
	if a.MaxUses != nil {
		m["maxUses"] = *a.MaxUses
	}
	if a.MaxTokens != nil {
		m["maxTokens"] = *a.MaxTokens
	}
	if a.Caching != nil {
		m["caching"] = map[string]interface{}{"type": a.Caching.Type, "ttl": a.Caching.TTL}
	}
	return m
}
