// Package toolbash is a first-party, provider-independent bash tool. It
// ports TypeScript @ai-sdk/tool-bash (experimental).
//
// The tool runs the model's commands in a sandbox session you supply
// (providerutils.SandboxSession, for example a Vercel sandbox from
// pkg/harness/sandbox/vercel). It never executes anything itself: with no
// sandbox, CreateBashTool fails. The caller owns the sandbox's lifecycle and
// is responsible for its isolation; the tool only forwards commands.
//
// Safety properties the tool enforces on top of the sandbox:
//   - Commands run only through the configured sandbox; there is no default
//     or local fallback.
//   - Each stream (stdout, stderr) is bounded: output beyond MaxOutputLength
//     characters (default 30,000) is discarded while it streams, so a noisy
//     command cannot exhaust memory, and a truncation notice is appended.
//   - An optional per-command timeout kills the command's process through
//     SandboxProcess.Kill (the whole process group on sandboxes that run
//     commands in one, such as the local harness sandbox) and returns exit
//     code 124.
package toolbash
