# Go AI SDK

## Project Overview

**Go AI SDK**: A Go toolkit for building AI-powered applications and agents, matching the Vercel AI SDK feature-for-feature in backend functionality.

### Problem

Go developers must write and maintain separate integration code for each AI model provider, because every provider differs in API design, authentication, and data formats.

### Approach

A single unified API abstracts provider differences behind consistent interfaces for text generation, streaming, structured data, embeddings, tool calling, and agent workflows. A pluggable provider architecture supports all major providers: OpenAI, Anthropic, Google, Azure, Bedrock, and others.

## Tech Stack

- **Language**: Go 1.24+
- **Package Manager**: Go modules
- **Testing**: `go test` with `github.com/stretchr/testify`
- **Linting**: `golangci-lint`
- **Build**: `go build`
- **Key Libraries**: OpenTelemetry (observability), google/uuid, various web frameworks (Fiber, Echo, Chi, Gin) for middleware integrations

## Project Structure

```
pkg/
  ai/          - Core AI functions (GenerateText, StreamText, GenerateObject, etc.)
  agent/       - Agent framework (skills, subagents, tool loops)
  provider/    - Provider interfaces and base types
  providers/   - Provider implementations (openai, anthropic, google, etc.)
  middleware/  - Language model middleware
  mcp/         - Model Context Protocol client
  schema/      - JSON schema validation
  registry/    - Provider registry
  telemetry/   - Telemetry utilities
  observability/ - MLflow integration
examples/      - Example applications
docs/          - Documentation
```

## ABSOLUTE RULES - NO EXCEPTIONS

### 1. Test-Driven Development is MANDATORY

**The Iron Law**: NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST

Every line of production code MUST follow this cycle:
1. **RED**: Write failing test FIRST
2. **Verify RED**: Run test, watch it fail for the RIGHT reason
3. **GREEN**: Write MINIMAL code to pass the test
4. **Verify GREEN**: Run test, confirm it passes
5. **REFACTOR**: Clean up with tests staying green

### 2. Violations = Delete and Start Over

If ANY of these occur, delete the code and start over:
- Wrote production code before test -> DELETE CODE, START OVER
- Test passed immediately -> TEST IS WRONG, FIX TEST FIRST
- Can't explain why test failed -> NOT TDD, START OVER
- "I'll add tests later" -> DELETE CODE NOW
- "Just this once without tests" -> NO. DELETE CODE.
- "It's too simple to test" -> NO. TEST FIRST.
- "Tests after achieve same goal" -> NO. DELETE CODE.

### 3. Test Coverage Requirements

- **Minimum 90%** coverage on ALL metrics:
  - Lines: 90%+
  - Functions: 90%+
  - Branches: 85%+
  - Statements: 90%+
- Coverage below threshold means the implementation is incomplete
- Untested code should not exist

### 4. Before Writing ANY Code

Ask yourself:
1. Did I write a failing test for this?
2. Did I run the test and see it fail?
3. Did it fail for the expected reason?

If ANY answer is "no" -> STOP. Write the test first.

### 5. Test File Structure

Go convention: place test files alongside production files:
- `pkg/ai/generate.go` -> `pkg/ai/generate_test.go`
- `pkg/provider/types/message.go` -> `pkg/provider/types/message_test.go`

### 6. Task Completion Requirements

**MANDATORY RULE**: A task is COMPLETE only when:
- ALL tests pass (100% green)
- The build succeeds with ZERO errors
- The linter reports ZERO errors or warnings
- Coverage meets minimum thresholds (90%+)
- Progress is documented in PROGRESS.md

A task with failing tests, build errors, or linter warnings remains INCOMPLETE.

### 7. Progress Documentation

After completing each task, record the following in `PROGRESS.md`:
```markdown
## Task X: [Name] - [COMPLETE/IN PROGRESS]
- Started: [timestamp]
- Tests: X passing, 0 failing
- Coverage: X%
- Build: Successful / Failed
- Linting: Clean / X errors
- Completed: [timestamp]
- Notes: [any relevant notes]
```

## Git Commit Rules

**COMMIT EARLY, COMMIT OFTEN** -- mandatory.

- Commit after every successful TDD cycle (RED-GREEN-REFACTOR)
- Commit after completing each discrete unit of work
- Commit before switching context or taking a break
- Keep uncommitted work under 30 minutes
- Make each commit atomic: one logical change per commit

Small commits simplify review and reversal. Frequent commits prevent lost work. Atomic commits make git history useful for debugging. Regular commits enforce small, testable increments.

### Branching Strategy

- Feature branches off `main`
- Branch naming: `feature/`, `fix/`, `chore/` prefixes (e.g., `feature/add-streaming-support`, `fix/anthropic-tool-parsing`)

### Commit Message Format

```
type(scope): brief description

- RED: What tests were written first
- GREEN: What minimal code was added
- Status: X tests passing, build successful
- Coverage: X% (if applicable)
```

## Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output and race detection
go test -v -race ./...

# Run tests for a specific package
go test ./pkg/ai/...

# Check coverage
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

# Build
go build ./...

# Lint
golangci-lint run

# Verify dependencies
go mod verify
```

## Red Flags - STOP Immediately

Stop immediately if you catch yourself:
- Opening a code file before its test file
- Writing an implementation before its test
- Thinking "I know this works"
- Copying code from examples without writing tests
- Skipping test runs
- Ignoring failing tests
- Writing multiple features before testing any

**STOP. DELETE. START WITH TEST.**

## The Mindset

- Tests are mandatory
- Tests come first, always
- Tests drive the implementation
- Untested code does not exist
- Coverage below 90% means the work is unfinished

## Verification

See @VERIFICATION_PLAN.md for acceptance testing procedures.
