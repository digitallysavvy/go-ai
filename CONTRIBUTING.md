# Contributing to the Go AI SDK

We deeply appreciate your interest in contributing to the Go AI SDK! Whether you're reporting bugs, suggesting enhancements, improving docs, or submitting pull requests, your contributions help improve the project for everyone.

## Reporting Bugs

If you've encountered a bug in the project, we encourage you to report it to us. Please follow these steps:

1. **Check the Issue Tracker**: Before submitting a new bug report, please check our issue tracker to see if the bug has already been reported. If it has, you can add to the existing report.
2. **Create a New Issue**: If the bug hasn't been reported, create a new issue. Provide a clear title and a detailed description of the bug. Include any relevant logs, error messages, and steps to reproduce the issue.
3. **Label Your Issue**: If possible, label your issue as a `bug` so it's easier for maintainers to identify.

## Suggesting Enhancements

We're always looking for suggestions to make our project better. If you have an idea for an enhancement, please:

1. **Check the Issue Tracker**: Similar to bug reports, please check if someone else has already suggested the enhancement. If so, feel free to add your thoughts to the existing issue.
2. **Create a New Issue**: If your enhancement hasn't been suggested yet, create a new issue. Provide a detailed description of your suggested enhancement and how it would benefit the project.

## Improving Documentation

Documentation is crucial for understanding and using our project effectively.
You can find the content of our docs under [`docs`](https://github.com/digitallysavvy/go-ai/tree/main/docs).

To fix smaller typos, you can edit the code directly in GitHub or use Github.dev (press `.` in Github).

If you want to make larger changes, please check out the Code Contributions section below. It also explains how to format and test the documentation changes.

## Code Contributions

We welcome your contributions to our code and documentation. Here's how you can contribute:

### Environment Setup

Go AI SDK development requires Go 1.22 or higher.

### Setting Up the Repository Locally

To set up the repository on your local machine, follow these steps:

1. **Fork the Repository**: Make a copy of the repository to your GitHub account.
2. **Clone the Repository**: Clone the repository to your local machine:
   ```bash
   git clone https://github.com/digitallysavvy/go-ai.git
   cd go-ai
   ```
3. **Install Go**: If you haven't already, install Go 1.22 or higher from [golang.org](https://golang.org/dl/).
4. **Install Dependencies**: Run `go mod download` to download all necessary dependencies.
5. **Verify Setup**: Run `go test ./...` to ensure everything is working correctly.

### Running the Examples

The project includes example applications in the `examples/` directory:

```bash
# Run a specific example
cd examples/text-generation
go run main.go

# Or run comprehensive example
cd examples/comprehensive
go run main.go
```

### Local Development Workflow

#### Testing

To test the entire project, run:

```bash
go test ./...
```

To test a specific package:

```bash
go test ./pkg/ai
go test ./pkg/middleware
```

To run tests with coverage:

```bash
go test ./... -cover
```

To generate a coverage report:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

To run tests in watch mode (requires a tool like `entr` or `modd`):

```bash
# Using entr
find . -name "*.go" | entr go test ./...
```

#### Building

To build the entire project:

```bash
go build ./...
```

To build a specific package:

```bash
go build ./pkg/ai
go build ./pkg/middleware
```

To build and verify the project compiles without errors:

```bash
go build ./...
```

To build with verbose output (useful for debugging):

```bash
go build -v ./...
```

To build with race detector enabled (for testing concurrent code):

```bash
go build -race ./...
```

To build for a specific platform:

```bash
# Build for Linux
GOOS=linux GOARCH=amd64 go build ./...

# Build for Windows
GOOS=windows GOARCH=amd64 go build ./...

# Build for macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build ./...
```

To build examples:

```bash
# Build a specific example
cd examples/text-generation
go build -o text-gen main.go

# Build all examples
for dir in examples/*/; do
  cd "$dir"
  go build -o "$(basename "$dir")" main.go
  cd ../..
done
```

To check if code compiles without building binaries:

```bash
go build -o /dev/null ./...
```

To build with specific build tags:

```bash
go build -tags=<tag> ./...
```

**Note**: Since this is a library package (not an executable), building primarily serves to verify that the code compiles correctly. The actual "build" happens when users `go get` or `go install` the package.

#### Code Formatting

Before committing, ensure your code is properly formatted:

```bash
go fmt ./...
```

We also recommend using `gofumpt` for stricter formatting:

```bash
go install mvdan.cc/gofumpt@latest
gofumpt -l -w .
```

#### Linting

We recommend using `golangci-lint` for comprehensive linting:

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run ./...
```

#### Adding Dependencies

When adding new dependencies, use:

```bash
go get <package>
go mod tidy
```

Always run `go mod tidy` before committing to ensure `go.mod` and `go.sum` are clean.

### Project Structure

```
go-ai/
├── pkg/
│   ├── ai/              # Main SDK package (public API)
│   ├── provider/        # Provider interface definitions
│   ├── providers/       # Provider implementations (30+)
│   ├── agent/           # Agent implementations
│   ├── middleware/      # Middleware system
│   ├── telemetry/       # OpenTelemetry integration
│   ├── registry/        # Model registry
│   ├── schema/          # JSON Schema validation
│   └── internal/        # Internal utilities
├── examples/            # Example applications
└── go.mod               # Go module definition
```

### Testing Guidelines

- Write tests for all new functionality
- Use table-driven tests where appropriate (Go best practice)
- Tests should be in `*_test.go` files co-located with source files
- Use `t.Parallel()` for tests that can run concurrently
- Mock external dependencies using the `testutil` package
- Aim for high test coverage, especially for core functionality

### Code Style Guidelines

- Follow standard Go formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and small
- Handle errors explicitly (don't ignore them)
- Use interfaces where appropriate for testability

### Adding a New Provider

If you're adding a new provider implementation:

1. Create a new directory under `pkg/providers/<provider-name>/`
2. Implement the `provider.Provider` interface
3. Implement the required model interfaces (`LanguageModel`, `EmbeddingModel`, etc.)
4. Add comprehensive tests
5. Update the README.md with the new provider
6. Add an example if applicable

### Submitting Pull Requests

We greatly appreciate your pull requests. Here are the steps to submit them:

1. **Create a New Branch**: Create a new branch for your changes:

   ```bash
   git checkout -b feature/your-feature-name
   ```

   Or for bug fixes:

   ```bash
   git checkout -b fix/your-bug-fix
   ```

2. **Make Your Changes**: Implement your changes following the guidelines above.

3. **Write Tests**: Ensure all new functionality has corresponding tests.

4. **Build the Project**: Ensure the code compiles:

   ```bash
   go build ./...
   ```

5. **Run Tests**: Make sure all tests pass:

   ```bash
   go test ./...
   ```

6. **Format Code**: Format your code:

   ```bash
   go fmt ./...
   ```

7. **Commit Your Changes**: Write clear, descriptive commit messages:

   ```bash
   git add .
   git commit -m "feat: add new feature description"
   ```

   We use conventional commit format:

   - `feat:` for new features
   - `fix:` for bug fixes
   - `docs:` for documentation changes
   - `test:` for test additions/changes
   - `refactor:` for code refactoring
   - `chore:` for maintenance tasks

8. **Push the Changes**: Push to your fork:

   ```bash
   git push origin feature/your-feature-name
   ```

9. **Open a Pull Request**: Create a pull request on GitHub with:

   - A clear title following the format: `feat(package): description` or `fix(package): description`
   - A detailed description of your changes
   - Reference any related issues
   - Include any breaking changes or migration notes

10. **Respond to Feedback**: Stay receptive to and address any feedback or alteration requests from the project maintainers.

### Pull Request Checklist

Before submitting your PR, ensure:

- [ ] Code compiles (`go build ./...`)
- [ ] All tests pass (`go test ./...`)
- [ ] Code is formatted (`go fmt ./...`)
- [ ] No linting errors
- [ ] Tests are added for new functionality
- [ ] Documentation is updated if needed
- [ ] Commit messages follow conventional commit format
- [ ] PR description is clear and comprehensive

Thank you for contributing to the Go AI SDK! Your efforts help us improve the project for everyone.
