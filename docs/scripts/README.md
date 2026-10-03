# Documentation Scripts

This directory contains utility scripts for validating and testing the Go-AI SDK documentation.

## Scripts

### compile-snippets.go

Compiles every ```go block in `docs/` against the module. Complete programs are vetted one by one and fail the run on any error; fragments are wrapped in a template and reported. Opt a block out with `skip-compile` in the fence info string. Run from the repository root:

```bash
go run docs/scripts/compile-snippets.go
```

See `docs/CONTRIBUTING_DOCS.md` for the policy.

### validate-links.go

Scans all documentation files and validates internal links.

**Features:**
- Finds all `.md` and `.mdx` files in the docs directory
- Extracts markdown links, reference components, and imports
- Validates that link targets exist
- Reports broken links with source file and line number
- Supports anchor links (`#section`)

**Usage:**

```bash
# Basic usage (from scripts directory)
go run validate-links.go

# Specify docs directory
go run validate-links.go -docs=../

# Verbose output
go run validate-links.go -docs=../ -verbose

# From project root
go run docs/scripts/validate-links.go -docs=docs/
```

**Flags:**
- `-docs`: Path to documentation root directory (default: `./`)
- `-verbose`: Enable verbose output showing all files found
- `-fix`: Attempt to fix broken links (not yet implemented)

**Example Output:**

```
🔍 Link Validation Report
=======================
Documentation root: ../

📊 Summary
----------
Total files scanned:   42
Total links found:     156
Broken links:          0

✅ All links are valid!
```

**Error Output:**

```
❌ Broken Links Found:
---------------------

📄 04-guides/streaming.mdx
   Line 45: [API Reference](../07-reference/api/stream.mdx)
           → Resolved to: 07-reference/api/stream.mdx
           → Error: Target file does not exist

💡 Suggestions:
  • Check if the target file exists
  • Verify the relative path is correct
  • Ensure the file extension (.mdx or .md) is included
  • Check for typos in the file name
```

### extract-examples.go

Extracts code examples from documentation and validates they compile.

**Features:**
- Finds all code blocks in documentation files
- Identifies complete Go examples (with package and functions)
- Extracts examples to separate files
- Attempts to compile examples
- Reports compilation errors with file and line number

**Usage:**

```bash
# Test examples without extracting to files
go run extract-examples.go -test-only

# Extract examples and test them
go run extract-examples.go -docs=../ -output=./extracted

# Verbose output
go run extract-examples.go -docs=../ -verbose

# Test specific file
go run extract-examples.go -docs=../04-guides/streaming.mdx

# Skip compilation (just extract)
go run extract-examples.go -compile=false
```

**Flags:**
- `-docs`: Path to documentation root directory (default: `./`)
- `-output`: Directory to output extracted examples (default: `./examples-extracted`)
- `-verbose`: Enable verbose output
- `-test-only`: Only test examples, don't extract to files
- `-compile`: Attempt to compile Go examples (default: `true`)

**Example Output:**

```
🔍 Code Example Extraction Report
=================================
Documentation root: ../

Found 38 code examples
  - 28 complete Go examples
  - 10 partial/snippet examples

✅ Extracted examples to: ./examples-extracted

🧪 Testing Examples
-------------------
✅ PASS: 04-guides/streaming.mdx (line 45)
✅ PASS: 04-guides/streaming.mdx (line 120)
✅ PASS: 03-providers/anthropic.mdx (line 67)

📊 Test Summary
---------------
Total examples found:    38
Examples tested:         28
Passed:                  28
Failed:                  0

✅ All tested examples compiled successfully!
```

**Error Output:**

```
❌ FAIL: 04-guides/example.mdx (line 45)

📊 Test Summary
---------------
Total examples found:    10
Examples tested:         8
Passed:                  7
Failed:                  1

❌ Failed Examples:
-------------------

📄 04-guides/example.mdx (line 45, block 2)
   Error: Compilation failed: exit status 2
   Compiler output:
      ./tmp123456.go:5:2: undefined: gai

💡 Common Issues:
  • Missing imports (add required packages)
  • Placeholder values (replace 'your-api-key' with actual values)
  • Pseudo-code (examples meant for illustration only)
  • Incomplete examples (snippets showing specific features)
```

## Common Workflows

### Before Committing Documentation

Always run both scripts to ensure quality:

```bash
cd docs/scripts

# Validate all links
go run validate-links.go -docs=../

# Test all examples
go run extract-examples.go -docs=../ -test-only
```

### Debugging a Specific File

```bash
cd docs/scripts

# Check links in specific file
go run validate-links.go -docs=../04-guides/your-file.mdx -verbose

# Test examples in specific file
go run extract-examples.go -docs=../04-guides/your-file.mdx -verbose
```

### CI/CD Integration

These scripts can be integrated into CI/CD pipelines:

```yaml
# GitHub Actions example
- name: Validate Documentation Links
  run: |
    cd docs/scripts
    go run validate-links.go -docs=../

- name: Test Documentation Examples
  run: |
    cd docs/scripts
    go run extract-examples.go -docs=../ -test-only
```

```makefile
# Makefile example
.PHONY: docs-validate
docs-validate:
	cd docs/scripts && go run validate-links.go -docs=../
	cd docs/scripts && go run extract-examples.go -docs=../ -test-only

.PHONY: docs-test
docs-test:
	cd docs/scripts && go run extract-examples.go -docs=../ -verbose
```

## Exit Codes

Both scripts follow standard exit code conventions:

- **0**: Success (no errors found)
- **1**: Failure (validation errors or broken links found)

This makes them suitable for use in CI/CD pipelines where non-zero exit codes fail the build.

## Development

### Adding New Validation Rules

To add new validation rules to `validate-links.go`:

1. Add new regex pattern to detect the link format
2. Extract links in `extractLinksFromFile()`
3. Add validation logic in `validateLinks()`
4. Update error reporting in `printReport()`

### Adding New Example Checks

To add new checks to `extract-examples.go`:

1. Modify `extractLinksFromFile()` to capture additional metadata
2. Add new validation in `isCompleteGoExample()`
3. Add new test cases in `testExample()`
4. Update reporting in `printTestResults()`

## Troubleshooting

### "Permission denied" errors

Make sure the scripts have execute permissions:

```bash
chmod +x validate-links.go extract-examples.go
```

Or always run with `go run`:

```bash
go run validate-links.go
go run extract-examples.go
```

### False positives in link validation

If a link is reported as broken but the file exists:

1. Check the relative path calculation
2. Verify file extension matches (`.md` vs `.mdx`)
3. Check for case sensitivity issues
4. Verify the file is not in an excluded directory

### Example compilation failures

Common reasons examples fail to compile:

1. **Missing imports**: Add all required import statements
2. **Placeholder values**: Examples with `"your-api-key"` may fail
3. **Pseudo-code**: Mark non-compilable code clearly in documentation
4. **External dependencies**: Examples requiring external services will fail

To mark examples as pseudo-code:

````markdown
```go
// This is pseudo-code for illustration only
result := DoMagic()
```
````

## Contributing

To improve these scripts:

1. Fork the repository
2. Make your changes
3. Test thoroughly with various documentation files
4. Submit a pull request with description of improvements

## License

These scripts are part of the Go-AI SDK and follow the same license.
