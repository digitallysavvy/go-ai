# Quality Tools Quick Reference

Quick reference for Go-AI SDK documentation quality tools and standards.

## Documentation Standards

### Style Guide
**File:** `DOCUMENTATION_STYLE_GUIDE.md`

**Key Standards:**
- File naming: lowercase-with-hyphens.mdx
- One H1 per page, maintain heading hierarchy
- Always include complete, runnable examples
- Use relative paths for internal links
- Include error handling in production examples

### Contribution Guide
**File:** `CONTRIBUTING_DOCS.md`

**Quick Start:**
1. Fork repository
2. Create branch: `git checkout -b docs/feature-name`
3. Use appropriate template from `_templates/`
4. Validate before committing
5. Submit PR

## Templates

Located in `_templates/` directory:

| Template | Use For | Size |
|----------|---------|------|
| `api-reference-template.mdx` | Types, functions, API docs | 7.7K |
| `provider-template.mdx` | AI provider documentation | 15K |
| `guide-template.mdx` | How-to guides, tutorials | 15K |
| `troubleshooting-template.mdx` | Problem-solution docs | 13K |

**Usage:**
```bash
cp docs/_templates/guide-template.mdx docs/04-guides/your-guide.mdx
```

## Validation Scripts

Located in `scripts/` directory. See `scripts/README.md` for full documentation.

### validate-links.go

**Purpose:** Validate all internal documentation links

**Quick Usage:**
```bash
cd docs/scripts

# Validate all links
go run validate-links.go -docs=../

# Verbose mode
go run validate-links.go -docs=../ -verbose
```

**Checks:**
- Markdown links: `[text](path.mdx)`
- Reference components: `<reference to="path" />`
- Relative path resolution
- File existence

### extract-examples.go

**Purpose:** Extract and test code examples from documentation

**Quick Usage:**
```bash
cd docs/scripts

# Test examples only (no file extraction)
go run extract-examples.go -docs=../ -test-only

# Extract and test with verbose output
go run extract-examples.go -docs=../ -verbose

# Test specific file
go run extract-examples.go -docs=../04-guides/file.mdx -verbose
```

**Checks:**
- Extracts all code blocks
- Identifies complete Go examples
- Attempts compilation
- Reports errors with line numbers

## Pre-Commit Workflow

**Before committing documentation changes:**

```bash
cd docs/scripts

# 1. Validate links
go run validate-links.go -docs=../

# 2. Test examples
go run extract-examples.go -docs=../ -test-only

# 3. If both pass, commit
cd ../..
git add docs/
git commit -m "docs: your commit message"
```

## Common Tasks

### Create New Guide
```bash
# 1. Copy template
cp docs/_templates/guide-template.mdx docs/04-guides/new-guide.mdx

# 2. Edit file (replace placeholders)

# 3. Validate
cd docs/scripts
go run validate-links.go -docs=../
go run extract-examples.go -docs=../ -verbose
```

### Create New Provider Doc
```bash
# 1. Copy template
cp docs/_templates/provider-template.mdx docs/03-providers/provider-name.mdx

# 2. Fill in provider details

# 3. Test examples
cd docs/scripts
go run extract-examples.go -docs=../03-providers/provider-name.mdx -verbose
```

### Create API Reference
```bash
# 1. Copy template
cp docs/_templates/api-reference-template.mdx docs/07-reference/api/type-name.mdx

# 2. Document the API

# 3. Validate
cd docs/scripts
go run validate-links.go -docs=../
go run extract-examples.go -docs=../ -test-only
```

### Fix Broken Links
```bash
# 1. Find broken links
cd docs/scripts
go run validate-links.go -docs=../ -verbose

# 2. Fix reported issues in source files

# 3. Re-validate
go run validate-links.go -docs=../
```

## Code Example Standards

### Minimal Complete Example
```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/teilomillet/go-ai"
)

func main() {
    client := gai.NewClient("your-api-key")

    response, err := client.Generate(context.Background(), &gai.Request{
        Model: "claude-3-5-sonnet-20241022",
        Messages: []gai.Message{
            {Role: "user", Content: "Hello!"},
        },
    })
    if err != nil {
        log.Fatalf("Error: %v", err)
    }

    fmt.Println(response.Content)
}
```

### Requirements
- ✅ Package declaration
- ✅ All necessary imports
- ✅ Complete, runnable code
- ✅ Error handling
- ✅ Meaningful comments
- ✅ Proper formatting

### Marking Pseudo-Code
````markdown
The following is pseudo-code for illustration:

```go
// Not meant to compile
result := DoSomething()
```
````

## File Naming

### Guides and Docs
- `streaming-responses.mdx` ✅
- `error-handling.mdx` ✅
- `StreamingResponses.mdx` ❌ (no camelCase)
- `streaming.mdx` ❌ (too vague)

### Numbered Sections
- `01-installation.mdx` ✅
- `02-quick-start.mdx` ✅
- `1-installation.mdx` ❌ (use two digits)

## Link Format

### Internal Links
```markdown
<!-- Relative paths -->
[Guide](../04-guides/streaming.mdx)
[API Reference](./api/client.mdx)

<!-- Include extension -->
[Provider](../03-providers/anthropic.mdx) ✅
[Provider](../03-providers/anthropic) ❌

<!-- Descriptive text -->
[streaming guide](./streaming.mdx) ✅
[click here](./streaming.mdx) ❌
```

## CI/CD Integration

### GitHub Actions Example
```yaml
name: Validate Documentation

on: [pull_request]

jobs:
  validate-docs:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Validate Links
        run: |
          cd docs/scripts
          go run validate-links.go -docs=../

      - name: Test Examples
        run: |
          cd docs/scripts
          go run extract-examples.go -docs=../ -test-only
```

### Makefile Example
```makefile
.PHONY: docs-validate
docs-validate:
	@echo "Validating documentation..."
	cd docs/scripts && go run validate-links.go -docs=../
	cd docs/scripts && go run extract-examples.go -docs=../ -test-only
	@echo "Documentation validation complete!"

.PHONY: docs-test
docs-test:
	cd docs/scripts && go run extract-examples.go -docs=../ -verbose
```

## Troubleshooting

### "Broken link" false positive
- Check file exists: `ls docs/path/to/file.mdx`
- Verify extension: `.md` vs `.mdx`
- Check case sensitivity
- Verify relative path

### Example compilation failure
- Check all imports are included
- Replace placeholder values (`your-api-key`)
- Mark as pseudo-code if intentional
- Verify Go syntax is valid

### Script errors
```bash
# Test script compilation
cd docs/scripts
go run validate-links.go -help
go run extract-examples.go -help

# Run with verbose for debugging
go run validate-links.go -docs=../ -verbose
go run extract-examples.go -docs=../ -verbose
```

## Resources

| Resource | Location |
|----------|----------|
| Style Guide | `docs/DOCUMENTATION_STYLE_GUIDE.md` |
| Contributing Guide | `docs/CONTRIBUTING_DOCS.md` |
| Templates | `docs/_templates/` |
| Scripts Documentation | `docs/scripts/README.md` |
| Validation Script | `docs/scripts/validate-links.go` |
| Example Testing Script | `docs/scripts/extract-examples.go` |

## Quick Checklist

Before submitting documentation PR:

- [ ] Used appropriate template
- [ ] All code examples compile
- [ ] Error handling included
- [ ] Links validated
- [ ] Examples tested
- [ ] Cross-references added
- [ ] Followed style guide
- [ ] No spelling/grammar errors

---

For detailed information, see the full documentation files referenced above.
