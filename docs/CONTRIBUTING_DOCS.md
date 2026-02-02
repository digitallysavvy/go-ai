# Contributing to Documentation

Thank you for your interest in improving the Go-AI SDK documentation! This guide will help you contribute effectively and ensure consistency across all documentation.

## Table of Contents

- [Getting Started](#getting-started)
- [Documentation Structure](#documentation-structure)
- [Writing Guidelines](#writing-guidelines)
- [Adding New Documentation](#adding-new-documentation)
- [Code Examples](#code-examples)
- [Testing Documentation](#testing-documentation)
- [Review Process](#review-process)
- [Common Tasks](#common-tasks)

## Getting Started

### Prerequisites

Before contributing to documentation, ensure you have:

- Go 1.21 or later installed
- Git configured on your system
- A text editor or IDE for editing Markdown files
- Basic understanding of Markdown syntax
- Familiarity with the Go-AI SDK (read the getting started guide)

### Initial Setup

1. **Fork the repository**:
   ```bash
   # Navigate to https://github.com/teilomillet/go-ai
   # Click the "Fork" button
   ```

2. **Clone your fork**:
   ```bash
   git clone https://github.com/YOUR-USERNAME/go-ai.git
   cd go-ai
   ```

3. **Create a documentation branch**:
   ```bash
   git checkout -b docs/your-feature-name
   ```

4. **Set up the development environment**:
   ```bash
   # Install Go dependencies
   go mod download

   # Verify documentation scripts work
   cd docs/scripts
   go run validate-links.go -docs=../ -verbose
   go run extract-examples.go -docs=../ -test-only
   ```

## Documentation Structure

The documentation is organized as follows:

```
docs/
├── 01-getting-started/        # Installation, quick start, basic setup
├── 02-core-concepts/          # Fundamental concepts and architecture
├── 03-providers/              # Provider-specific documentation
├── 04-guides/                 # How-to guides and tutorials
├── 05-examples/               # Complete working examples
├── 06-troubleshooting/        # Common issues and solutions
├── 07-reference/              # API reference documentation
│   ├── api/                   # API types and functions
│   └── types/                 # Type definitions
├── _templates/                # Templates for new documentation
├── scripts/                   # Validation and testing scripts
├── DOCUMENTATION_STYLE_GUIDE.md
└── CONTRIBUTING_DOCS.md       # This file
```

### Directory Guidelines

- **01-getting-started/**: Beginner-focused content, minimal prerequisites
- **02-core-concepts/**: Explanations of SDK architecture and design
- **03-providers/**: One file per provider with complete usage guide
- **04-guides/**: Task-oriented documentation (how to accomplish X)
- **05-examples/**: Full, runnable examples demonstrating real use cases
- **06-troubleshooting/**: Problem-solution format documentation
- **07-reference/**: Auto-generated or manually maintained API docs

## Writing Guidelines

### Style Guide

All documentation must follow our [Documentation Style Guide](./DOCUMENTATION_STYLE_GUIDE.md). Key points:

- Use clear, concise language
- Write in second person ("you")
- Include complete, runnable examples
- Add error handling in production examples
- Link to related documentation
- Follow markdown formatting standards

### File Naming

- Use lowercase with hyphens: `streaming-responses.mdx`
- Be descriptive: `error-handling.mdx` not `errors.mdx`
- Use numbered prefixes for ordered sections: `01-installation.mdx`
- Always use `.mdx` extension

### Markdown Formatting

```markdown
# Page Title (H1 - only one per page)

Brief description of what this page covers.

## Main Section (H2)

Content for this section.

### Subsection (H3)

More specific content.

## Another Section

Content here.
```

### Code Examples

All code examples must:
- Be syntactically correct
- Include necessary imports
- Be complete and runnable (unless marked as snippet)
- Include error handling
- Use meaningful variable names
- Have explanatory comments

Example:

````markdown
```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/teilomillet/go-ai"
)

func main() {
    // Create client with API key
    client := gai.NewClient("your-api-key")

    // Create request
    request := &gai.Request{
        Model: "claude-3-5-sonnet-20241022",
        Messages: []gai.Message{
            {Role: "user", Content: "Hello!"},
        },
    }

    // Generate response
    response, err := client.Generate(context.Background(), request)
    if err != nil {
        log.Fatalf("Error: %v", err)
    }

    fmt.Println(response.Content)
}
```
````

## Adding New Documentation

### Step 1: Choose the Right Template

Start with the appropriate template from `_templates/`:

- **API Reference**: Use `api-reference-template.mdx`
- **Provider Documentation**: Use `provider-template.mdx`
- **How-to Guide**: Use `guide-template.mdx`
- **Troubleshooting**: Use `troubleshooting-template.mdx`

```bash
# Copy template to your new file
cp docs/_templates/guide-template.mdx docs/04-guides/your-new-guide.mdx
```

### Step 2: Fill in the Template

Follow the template structure and instructions. Replace all placeholders:

- `[Guide Title]` → Your actual title
- `[Section Name]` → Your section names
- Example code → Your working examples
- `<!-- INSTRUCTIONS -->` → Remove these comments

### Step 3: Add Cross-References

Link to related documentation:

```markdown
For more details on configuration, see the [Client Configuration guide](../02-core-concepts/client-configuration.mdx).

This feature requires understanding [streaming responses](./streaming-responses.mdx).
```

### Step 4: Update Navigation

If adding a new major section, update relevant index pages:

```markdown
## Related Topics

- [Your New Guide](./your-new-guide.mdx)
- [Existing Guide](./existing-guide.mdx)
```

### Step 5: Validate Your Changes

Run validation scripts before committing:

```bash
# Validate internal links
cd docs/scripts
go run validate-links.go -docs=../ -verbose

# Test code examples
go run extract-examples.go -docs=../ -verbose
```

## Code Examples

### Writing Examples

1. **Keep examples focused**: Each example should demonstrate one concept
2. **Make examples runnable**: Include all necessary code
3. **Add context**: Explain what the example does and why
4. **Handle errors**: Show proper error handling patterns
5. **Use realistic data**: Avoid foo/bar, use meaningful examples

### Example Structure

```go
package main

import (
    // Standard library imports
    "context"
    "fmt"
    "log"

    // Third-party imports
    "github.com/teilomillet/go-ai"
)

func main() {
    // 1. Setup
    client := gai.NewClient("your-api-key")

    // 2. Main operation
    result, err := client.DoSomething(context.Background())
    if err != nil {
        log.Fatalf("Error: %v", err)
    }

    // 3. Use result
    fmt.Println(result)
}
```

### Testing Examples

Your examples will be automatically extracted and tested. Ensure they compile:

```bash
# Test your specific file
cd docs/scripts
go run extract-examples.go -docs=../04-guides/your-guide.mdx -verbose

# Test all examples
go run extract-examples.go -docs=../ -compile
```

### Marking Non-Compilable Examples

If your example is pseudo-code or intentionally incomplete:

````markdown
```go
// This is pseudo-code for illustration
client := NewClient()
result := client.DoMagic() // Simplified for clarity
```
````

Add a note above the example:

```markdown
The following is pseudo-code showing the general pattern:

```go
// pseudo-code here
```
```

## Testing Documentation

### Manual Testing

1. **Read through your changes**: Catch obvious errors
2. **Click all links**: Ensure they point to correct locations
3. **Copy-paste examples**: Verify they work as written
4. **Check formatting**: Preview how it renders

### Automated Testing

Run these scripts before submitting:

```bash
cd docs/scripts

# 1. Validate all links
go run validate-links.go -docs=../

# 2. Extract and test examples
go run extract-examples.go -docs=../ -compile

# 3. Run with verbose output for detailed feedback
go run validate-links.go -docs=../ -verbose
go run extract-examples.go -docs=../ -verbose
```

### Expected Output

**Successful validation:**
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

**Successful example testing:**
```
🔍 Code Example Extraction Report
=================================

Found 38 code examples
  - 28 complete Go examples
  - 10 partial/snippet examples

🧪 Testing Examples
-------------------
✅ PASS: 04-guides/streaming-responses.mdx (line 45)
✅ PASS: 04-guides/streaming-responses.mdx (line 120)
...

📊 Test Summary
---------------
Total examples found:    38
Examples tested:         28
Passed:                  28
Failed:                  0

✅ All tested examples compiled successfully!
```

### Fixing Test Failures

If validation fails:

1. **Link validation failures**: Check file paths are correct and files exist
2. **Compilation failures**: Ensure examples have all imports and valid syntax
3. **Missing imports**: Add all required import statements
4. **Placeholder issues**: Examples with `your-api-key` may fail - this is expected

## Review Process

### Before Submitting

Complete this checklist:

- [ ] Read the [Documentation Style Guide](./DOCUMENTATION_STYLE_GUIDE.md)
- [ ] Used appropriate template from `_templates/`
- [ ] All code examples compile and run
- [ ] Links validated with `validate-links.go`
- [ ] Examples tested with `extract-examples.go`
- [ ] Cross-references added to related docs
- [ ] Navigation/index pages updated if needed
- [ ] Followed markdown formatting standards
- [ ] Grammar and spelling checked

### Submitting Your Changes

1. **Commit your changes**:
   ```bash
   git add docs/
   git commit -m "docs: add guide for feature X

   - Add comprehensive guide for feature X
   - Include working examples
   - Add troubleshooting section
   - Update related documentation links"
   ```

2. **Push to your fork**:
   ```bash
   git push origin docs/your-feature-name
   ```

3. **Create a Pull Request**:
   - Go to the original repository
   - Click "New Pull Request"
   - Select your fork and branch
   - Fill in the PR template
   - Link related issues if applicable

### PR Template

```markdown
## Description

Brief description of what documentation you're adding/changing.

## Type of Change

- [ ] New documentation
- [ ] Documentation update
- [ ] Fix typos/errors
- [ ] Restructure existing docs
- [ ] Add examples

## Checklist

- [ ] Followed style guide
- [ ] Used appropriate template
- [ ] All examples compile
- [ ] Links validated
- [ ] Cross-references added
- [ ] No spelling/grammar errors

## Related Issues

Closes #123
```

### Review Criteria

Reviewers will check:

1. **Accuracy**: Information is correct and up-to-date
2. **Completeness**: All necessary information is included
3. **Clarity**: Content is easy to understand
4. **Examples**: Code examples work and demonstrate concepts well
5. **Style**: Follows documentation style guide
6. **Links**: All links work and point to correct locations
7. **Structure**: Content is well-organized

### Addressing Feedback

- Respond to all comments
- Make requested changes
- Re-run validation scripts after changes
- Mark conversations as resolved when addressed
- Be open to suggestions and improvements

## Common Tasks

### Adding a New Guide

```bash
# 1. Copy template
cp docs/_templates/guide-template.mdx docs/04-guides/new-feature.mdx

# 2. Edit the file
# - Fill in all sections
# - Add working examples
# - Add cross-references

# 3. Validate
cd docs/scripts
go run validate-links.go -docs=../
go run extract-examples.go -docs=../ -compile

# 4. Commit and push
git add docs/04-guides/new-feature.mdx
git commit -m "docs: add guide for new feature"
git push origin docs/new-feature
```

### Adding Provider Documentation

```bash
# 1. Copy provider template
cp docs/_templates/provider-template.mdx docs/03-providers/new-provider.mdx

# 2. Fill in provider-specific details
# - Authentication setup
# - Available models
# - Provider-specific options
# - Complete examples

# 3. Test examples thoroughly
cd docs/scripts
go run extract-examples.go -docs=../03-providers/new-provider.mdx -verbose

# 4. Submit PR
```

### Updating Existing Documentation

```bash
# 1. Create branch
git checkout -b docs/update-existing-doc

# 2. Make changes
# Edit the existing file

# 3. Verify no broken links introduced
cd docs/scripts
go run validate-links.go -docs=../ -verbose

# 4. Test affected examples
go run extract-examples.go -docs=../path/to/file.mdx -verbose

# 5. Commit and push
git add docs/path/to/file.mdx
git commit -m "docs: update existing documentation

- Add missing information
- Fix outdated examples
- Improve clarity"
git push origin docs/update-existing-doc
```

### Fixing Broken Links

```bash
# 1. Find broken links
cd docs/scripts
go run validate-links.go -docs=../ -verbose

# 2. Fix each broken link
# - Update file paths
# - Fix typos
# - Add missing files if needed

# 3. Re-validate
go run validate-links.go -docs=../

# 4. Commit fix
git add docs/
git commit -m "docs: fix broken links in documentation"
```

### Adding API Reference

```bash
# 1. Copy API reference template
cp docs/_templates/api-reference-template.mdx docs/07-reference/api/new-type.mdx

# 2. Document the API
# - Type definition
# - All methods
# - Options
# - Complete examples

# 3. Test all examples
cd docs/scripts
go run extract-examples.go -docs=../07-reference/api/new-type.mdx -verbose

# 4. Add to reference index
# Edit docs/07-reference/README.mdx to include new type

# 5. Submit PR
```

## Tips for Great Documentation

### Do's

✅ **Do** write in clear, simple language
✅ **Do** include complete, working examples
✅ **Do** explain why, not just what
✅ **Do** add error handling to examples
✅ **Do** link to related documentation
✅ **Do** test all code examples
✅ **Do** use consistent formatting
✅ **Do** provide context and prerequisites

### Don'ts

❌ **Don't** assume prior knowledge
❌ **Don't** use jargon without explanation
❌ **Don't** skip error handling in examples
❌ **Don't** forget to validate links
❌ **Don't** leave placeholders uncommented
❌ **Don't** make examples too complex
❌ **Don't** forget cross-references

### Writing Tips

1. **Start with the end in mind**: What should the reader be able to do after reading?
2. **Show, don't tell**: Use examples to demonstrate concepts
3. **Be concise**: Every word should add value
4. **Use active voice**: "Configure the client" not "The client should be configured"
5. **Test everything**: If you can't test it, mark it clearly as pseudo-code

## Getting Help

### Resources

- [Documentation Style Guide](./DOCUMENTATION_STYLE_GUIDE.md)
- [Template Files](./_templates/)
- [Existing Documentation](./01-getting-started/)
- [Go-AI Repository](https://github.com/teilomillet/go-ai)

### Questions?

- Open a [GitHub Discussion](https://github.com/teilomillet/go-ai/discussions)
- Ask in the [Community Forum](https://community.example.com)
- Comment on related issues or PRs
- Reach out to maintainers

### Found an Issue?

If you find errors in existing documentation:

1. Check if an issue already exists
2. Create a new issue with:
   - Location of the error (file and line number)
   - Description of the problem
   - Suggested fix (if you have one)
3. Or submit a PR with the fix directly

## Recognition

Contributors to documentation will be:
- Listed in the contributors file
- Credited in release notes for significant contributions
- Recognized in the community

Thank you for helping make Go-AI documentation better!

---

**Questions or suggestions about this guide?** Open an issue or submit a PR to improve it!
