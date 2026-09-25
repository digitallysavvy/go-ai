package harness

import (
	"encoding/json"
	"errors"
)

// Shared authentication modes (TS `HarnessV1Authentication`). Adapters add
// their own concrete choices (for example "direct" or "subscription").
const (
	AuthModeAuto      = "auto"
	AuthModeAIGateway = "ai-gateway"
	AuthModeDirect    = "direct"
)

// ErrInvalidAuthentication mirrors the TS TypeError message thrown for
// non-flat authentication records.
var ErrInvalidAuthentication = errors.New("Invalid auth: expected an authentication mode or a flat record with string values.")

// Authentication is an adapter `auth` setting: either a mode string ("auto",
// "ai-gateway" or an adapter choice) or an isolated authentication
// environment that replaces the host process environment for authentication
// discovery. The zero value means "not configured" (TS undefined).
type Authentication struct {
	Mode        string
	Environment map[string]string
}

// AuthMode returns an Authentication selecting mode.
func AuthMode(mode string) Authentication { return Authentication{Mode: mode} }

// AuthEnvironment returns an Authentication backed by an isolated environment.
// A nil env is treated as an empty record.
func AuthEnvironment(env map[string]string) Authentication {
	if env == nil {
		env = map[string]string{}
	}
	return Authentication{Environment: env}
}

// IsEnvironment reports whether the authentication is an isolated
// environment (TS `isHarnessAuthenticationEnvironment`).
func (a Authentication) IsEnvironment() bool { return a.Environment != nil }

// IsZero reports whether no authentication was configured.
func (a Authentication) IsZero() bool { return a.Mode == "" && a.Environment == nil }

// MarshalJSON encodes a mode string or a flat record.
func (a Authentication) MarshalJSON() ([]byte, error) {
	if a.Environment != nil {
		return json.Marshal(a.Environment)
	}
	if a.Mode == "" {
		return []byte("null"), nil
	}
	return json.Marshal(a.Mode)
}

// UnmarshalJSON accepts a mode string or a flat record of strings.
func (a *Authentication) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*a = Authentication{}
		return nil
	}
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		*a = Authentication{Mode: mode}
		return nil
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return ErrInvalidAuthentication
	}
	*a = AuthEnvironment(env)
	return nil
}
