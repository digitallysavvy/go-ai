package ai

// IDGenerator returns a stable identifier for tests and integrations that need
// deterministic response or call IDs.
type IDGenerator func() string

// InternalOptions mirrors the TypeScript SDK _internal option. It is intended
// for tests and framework integrations and may change without notice.
type InternalOptions struct {
	GenerateID     IDGenerator
	GenerateCallID IDGenerator
}

func internalGenerateID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateID != nil {
		return internal.GenerateID
	}
	return newCallID
}

func internalGenerateCallID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateCallID != nil {
		return internal.GenerateCallID
	}
	return newCallID
}
