// Package toolwebfetch is a first-party, provider-independent web fetch tool.
// It ports TypeScript @ai-sdk/tool-web-fetch.
//
// The model calls the tool with a URL. HTML pages are converted to markdown
// with the main content extracted, and text formats are returned as is.
// JavaScript is not executed. Failures are returned as results so the model
// can recover.
//
// The tool fetches untrusted URLs with the SDK's SSRF-safe download
// validation (the same checks as ai.Download):
//   - Requests are GET only, without cookies or credentials, and URLs that
//     contain credentials are rejected.
//   - Every hop, including each redirect, is checked against the domain
//     filter and against local, private and reserved addresses, including the
//     .local, .localhost and .internal suffixes and IDN spellings of blocked
//     hostnames.
//   - The default transport validates and pins DNS results at connect time.
//   - Redirects are followed manually (at most 5) so each target is checked
//     before it is requested.
//   - The response size and the time per call are bounded.
//
// Error messages never repeat the requested URL, so a rejected URL cannot
// authorize itself by appearing in a tool result.
package toolwebfetch
