package tools

import "fmt"

// validateWebSearchLocation mirrors the TS args schemas' userLocation.type
// z.literal('approximate'), which validateTypes rejects before the request.
func validateWebSearchLocation(tool string, loc *WebSearchUserLocation) error {
	if loc != nil && loc.Type != "approximate" {
		return fmt.Errorf("%s userLocation.type must be \"approximate\", got %q", tool, loc.Type)
	}
	return nil
}

// validateResponseInclusion mirrors z.enum(['full', 'excluded']).
func validateResponseInclusion(tool, v string) error {
	if v != "" && v != "full" && v != "excluded" {
		return fmt.Errorf("%s responseInclusion must be \"full\" or \"excluded\", got %q", tool, v)
	}
	return nil
}

// Validate implements the prepareTools args-validation hook (TS validateTypes).
func (o *webSearch20250305Opts) Validate() error {
	return validateWebSearchLocation("web_search_20250305", o.Config.UserLocation)
}

// Validate implements the prepareTools args-validation hook (TS validateTypes).
func (o *webSearch20260209Opts) Validate() error {
	return validateWebSearchLocation("web_search_20260209", o.Config.UserLocation)
}

// Validate implements the prepareTools args-validation hook (TS validateTypes).
func (o *webSearch20260318Opts) Validate() error {
	if err := validateWebSearchLocation("web_search_20260318", o.Config.UserLocation); err != nil {
		return err
	}
	return validateResponseInclusion("web_search_20260318", o.Config.ResponseInclusion)
}

// Validate implements the prepareTools args-validation hook (TS validateTypes).
func (o *webFetch20260318Opts) Validate() error {
	return validateResponseInclusion("web_fetch_20260318", o.Config.ResponseInclusion)
}
