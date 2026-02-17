# Verification Plan

## Prerequisites

- Go 1.24+ installed
- `golangci-lint` installed
- All dependencies downloaded (`go mod download`)

## Scenarios

### Scenario 1: All Tests Pass

**Context**: Working directory is the project root. Dependencies are downloaded.

**Steps**:
1. Run `go mod verify` to confirm dependency integrity
2. Run `go test -v -race ./...` to execute all tests with race detection

**Success Criteria**:
- [ ] `go mod verify` reports all modules verified
- [ ] All tests pass (zero failures)
- [ ] No data races detected

**If Blocked**: If tests fail due to missing external services or API keys, note which tests require them and verify all other tests pass. Ask the developer for guidance on provider-specific tests.

### Scenario 2: Build Succeeds

**Context**: Working directory is the project root.

**Steps**:
1. Run `go build ./...` to compile all packages
2. Run `go vet ./...` to check for suspicious constructs

**Success Criteria**:
- [ ] Build completes with zero errors
- [ ] `go vet` reports no issues

**If Blocked**: If build fails due to missing system dependencies, document the exact error and ask the developer.

### Scenario 3: Linter Passes

**Context**: `golangci-lint` is installed. Working directory is the project root.

**Steps**:
1. Run `golangci-lint run --timeout=5m`

**Success Criteria**:
- [ ] Linter reports zero errors
- [ ] Linter reports zero warnings

**If Blocked**: If `golangci-lint` is not installed, install it and retry. If specific lint rules conflict with project conventions, ask the developer.

### Scenario 4: Examples Compile

**Context**: Working directory is the project root. All examples are in `examples/`.

**Steps**:
1. For each directory in `examples/` containing a `main.go`, run `go build .`

**Success Criteria**:
- [ ] Every example with a `main.go` compiles without errors

**If Blocked**: If an example requires credentials or external configuration to compile, note it and continue with the remaining examples. Ask the developer if more than two examples fail.

## Verification Rules

- Never use mocks or fakes during verification
- Test environments must be fully running copies of real systems
- If any success criterion fails, verification fails
- Ask the developer for help if blocked; do not guess
