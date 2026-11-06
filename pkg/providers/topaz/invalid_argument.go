package topaz

import providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

// invalidArgument is TS InvalidArgumentError ("Invalid argument for parameter
// <field>: <message>"). Its cause is the *ValidationError v0.5.2 returned for
// the same failures, so callers matching either type with errors.As keep
// working.
func invalidArgument(field, message string, cause error) error {
	return providererrors.NewArgumentError(field, message, providererrors.NewValidationError(field, message, cause))
}
