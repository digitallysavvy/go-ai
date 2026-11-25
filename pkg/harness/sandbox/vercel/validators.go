package vercel

import (
	"regexp"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// The @vercel/sandbox 3.9.0 response schemas (dist/api-client/validators.js)
// that base-client.js parse() applies to every API response through
// parseOrThrow. Only the responses this client reads are ported. Objects are
// non-strict (zod strips or, with passthrough, keeps unknown keys; neither
// changes validation), `.optional()` accepts an absent key but not null.

var (
	zStr  = zodlite.String
	zNum  = zodlite.Number
	zBool = zodlite.Boolean
)

func zOpt(n zodlite.Node) zodlite.Node { return zodlite.Optional(n) }

// vercelRuleMatcher is RuleMatcherValidator.
var vercelRuleMatcher = zodlite.Object(
	zodlite.F("exact", zOpt(zStr)),
	zodlite.F("startsWith", zOpt(zStr)),
	zodlite.F("regex", zOpt(zStr)),
)

// vercelKeyValueMatcher is KeyValueMatcherValidator.
var vercelKeyValueMatcher = zodlite.Object(
	zodlite.F("key", zOpt(vercelRuleMatcher)),
	zodlite.F("value", zOpt(vercelRuleMatcher)),
)

// vercelRuleMatch is RuleMatchValidator.
var vercelRuleMatch = zodlite.Object(
	zodlite.F("path", zOpt(vercelRuleMatcher)),
	zodlite.F("method", zOpt(zodlite.Array(zStr))),
	zodlite.F("queryString", zOpt(zodlite.Array(vercelKeyValueMatcher))),
	zodlite.F("headers", zOpt(zodlite.Array(vercelKeyValueMatcher))),
)

// vercelNetworkPolicyResponse is NetworkPolicyResponseValidator: the
// allow-all / deny-all mode objects or the legacy custom policy.
var vercelNetworkPolicyResponse = zodlite.Union(
	zodlite.Object(zodlite.F("mode", zodlite.Literal("allow-all"))),
	zodlite.Object(zodlite.F("mode", zodlite.Literal("deny-all"))),
	zodlite.Object(
		zodlite.F("mode", zodlite.Literal("custom")),
		zodlite.F("allowedDomains", zOpt(zodlite.Array(zStr))),
		zodlite.F("allowedCIDRs", zOpt(zodlite.Array(zStr))),
		zodlite.F("deniedCIDRs", zOpt(zodlite.Array(zStr))),
		zodlite.F("injectionRules", zOpt(zodlite.Array(zodlite.Object(
			zodlite.F("domain", zStr),
			zodlite.F("headers", zOpt(zodlite.RecordOf(zStr))),
			zodlite.F("headerNames", zOpt(zodlite.Array(zStr))),
			zodlite.F("match", zOpt(vercelRuleMatch)),
		)))),
		zodlite.F("forwardRules", zOpt(zodlite.Array(zodlite.Object(
			zodlite.F("domain", zStr),
			zodlite.F("forwardURL", zStr),
			zodlite.F("match", zOpt(vercelRuleMatch)),
		)))),
	),
)

var vercelStatusEnum = zodlite.Enum("pending", "running", "stopping", "stopped", "failed", "aborted", "snapshotting")

// vercelSession is Session.
var vercelSession = zodlite.Object(
	zodlite.F("id", zStr),
	zodlite.F("memory", zNum),
	zodlite.F("vcpus", zNum),
	zodlite.F("region", zStr),
	zodlite.F("runtime", zOpt(zStr)),
	zodlite.F("timeout", zNum),
	zodlite.F("status", vercelStatusEnum),
	zodlite.F("requestedAt", zNum),
	zodlite.F("startedAt", zOpt(zNum)),
	zodlite.F("requestedStopAt", zOpt(zNum)),
	zodlite.F("stoppedAt", zOpt(zNum)),
	zodlite.F("abortedAt", zOpt(zNum)),
	zodlite.F("duration", zOpt(zNum)),
	zodlite.F("sourceSnapshotId", zOpt(zStr)),
	zodlite.F("snapshottedAt", zOpt(zNum)),
	zodlite.F("createdAt", zNum),
	zodlite.F("cwd", zStr),
	zodlite.F("updatedAt", zNum),
	zodlite.F("interactivePort", zOpt(zNum)),
	zodlite.F("networkPolicy", zOpt(vercelNetworkPolicyResponse)),
	zodlite.F("activeCpuDurationMs", zOpt(zNum)),
	zodlite.F("networkTransfer", zOpt(zodlite.Object(
		zodlite.F("ingress", zNum),
		zodlite.F("egress", zNum),
	))),
)

// vercelSandboxRoute is SandboxRoute.
var vercelSandboxRoute = zodlite.Object(
	zodlite.F("url", zStr),
	zodlite.F("subdomain", zStr),
	zodlite.F("port", zNum),
)

// vercelSnapshot is Snapshot.
var vercelSnapshot = zodlite.Object(
	zodlite.F("id", zStr),
	zodlite.F("sourceSessionId", zStr),
	zodlite.F("region", zStr),
	zodlite.F("regions", zOpt(zodlite.Array(zStr))),
	zodlite.F("status", zodlite.Enum("created", "deleted", "failed")),
	zodlite.F("sizeBytes", zNum),
	zodlite.F("expiresAt", zOpt(zNum)),
	zodlite.F("createdAt", zNum),
	zodlite.F("updatedAt", zNum),
	zodlite.F("lastUsedAt", zOpt(zNum)),
	zodlite.F("creationMethod", zOpt(zStr)),
	zodlite.F("parentId", zOpt(zStr)),
)

// vercelCursorPagination is CursorPagination.
var vercelCursorPagination = zodlite.Object(
	zodlite.F("count", zNum),
	zodlite.F("next", zodlite.Nullable(zStr)),
)

// vercelCommandFields are Command's keys; CommandFinished requires a
// numeric exitCode.
func vercelCommand(exitCode zodlite.Node) zodlite.Node {
	return zodlite.Object(
		zodlite.F("id", zStr),
		zodlite.F("name", zStr),
		zodlite.F("args", zodlite.Array(zStr)),
		zodlite.F("cwd", zStr),
		zodlite.F("sessionId", zStr),
		zodlite.F("exitCode", exitCode),
		zodlite.F("durationMs", zOpt(zNum)),
		zodlite.F("startedAt", zNum),
	)
}

// vercelSandbox is Sandbox.
var vercelSandbox = zodlite.Object(
	zodlite.F("name", zStr),
	zodlite.F("persistent", zBool),
	zodlite.F("region", zOpt(zStr)),
	zodlite.F("failoverRegions", zOpt(zodlite.Array(zStr))),
	zodlite.F("networkId", zOpt(zStr)),
	zodlite.F("vcpus", zOpt(zNum)),
	zodlite.F("memory", zOpt(zNum)),
	zodlite.F("runtime", zOpt(zStr)),
	zodlite.F("image", zOpt(zStr)),
	zodlite.F("timeout", zOpt(zNum)),
	zodlite.F("networkPolicy", zOpt(vercelNetworkPolicyResponse)),
	zodlite.F("totalEgressBytes", zOpt(zNum)),
	zodlite.F("totalIngressBytes", zOpt(zNum)),
	zodlite.F("totalActiveCpuDurationMs", zOpt(zNum)),
	zodlite.F("totalDurationMs", zOpt(zNum)),
	zodlite.F("createdAt", zNum),
	zodlite.F("updatedAt", zNum),
	zodlite.F("expiresAt", zOpt(zNum)),
	zodlite.F("currentSessionId", zStr),
	zodlite.F("currentSnapshotId", zOpt(zStr)),
	zodlite.F("status", vercelStatusEnum),
	zodlite.F("statusUpdatedAt", zOpt(zNum)),
	zodlite.F("cwd", zOpt(zStr)),
	zodlite.F("tags", zOpt(zodlite.RecordOf(zStr))),
	zodlite.F("mounts", zOpt(zodlite.RecordOf(zodlite.Object(
		zodlite.F("drive", zStr),
		zodlite.F("mode", zOpt(zodlite.Enum("snapshot", "read-write", "read-only"))),
	)))),
	zodlite.F("snapshotExpiration", zOpt(zNum)),
	zodlite.F("keepLastSnapshots", zOpt(zodlite.Object(
		zodlite.F("count", zNum),
		zodlite.F("expiration", zOpt(zNum)),
		zodlite.F("deleteEvicted", zOpt(zBool)),
	))),
)

// The response schemas parseOrThrow applies, by 3.9.0 APIClient method.
var (
	// vercelSandboxAndSessionResponse: createSandbox, getSandbox.
	vercelSandboxAndSessionResponse = zodlite.Object(
		zodlite.F("sandbox", vercelSandbox),
		zodlite.F("session", vercelSession),
		zodlite.F("routes", zodlite.Array(vercelSandboxRoute)),
		zodlite.F("resumed", zOpt(zBool)),
	)
	// vercelUpdateSandboxResponse: updateSandbox, deleteSandbox.
	vercelUpdateSandboxResponse = zodlite.Object(
		zodlite.F("sandbox", vercelSandbox),
		zodlite.F("routes", zOpt(zodlite.Array(vercelSandboxRoute))),
	)
	// vercelStopSessionResponse: stopSession.
	vercelStopSessionResponse = zodlite.Object(
		zodlite.F("session", vercelSession),
		zodlite.F("sandbox", zOpt(vercelSandbox)),
		zodlite.F("snapshot", zOpt(vercelSnapshot)),
	)
	// vercelCreateSnapshotResponse: createSnapshot.
	vercelCreateSnapshotResponse = zodlite.Object(
		zodlite.F("snapshot", vercelSnapshot),
		zodlite.F("session", vercelSession),
	)
	// vercelSnapshotsResponse: listSnapshots.
	vercelSnapshotsResponse = zodlite.Object(
		zodlite.F("snapshots", zodlite.Array(vercelSnapshot)),
		zodlite.F("pagination", vercelCursorPagination),
	)
	// vercelSessionsResponse: listSessions.
	vercelSessionsResponse = zodlite.Object(
		zodlite.F("sessions", zodlite.Array(vercelSession)),
		zodlite.F("pagination", vercelCursorPagination),
	)
	// vercelSessionResponse: extendTimeout, updateNetworkPolicy.
	vercelSessionResponse = zodlite.Object(zodlite.F("session", vercelSession))
	// vercelCommandResponse: runCommand (detached, and the first stream
	// record), getCommand, killCommand.
	vercelCommandResponse = zodlite.Object(zodlite.F("command", vercelCommand(zodlite.Nullable(zNum))))
	// vercelCommandFinishedResponse: getCommand({wait}) and the finished
	// stream record.
	vercelCommandFinishedResponse = zodlite.Object(zodlite.F("command", vercelCommand(zNum)))
	// vercelCommandStdinResponse: writeCommandStdin.
	vercelCommandStdinResponse = zodlite.Object(
		zodlite.F("command", vercelCommand(zodlite.Nullable(zNum))),
		zodlite.F("bytesWritten", zOpt(zodlite.Min(zodlite.Int, 0))),
	)
	// vercelEmptyResponse: mkDir, writeFiles.
	vercelEmptyResponse = zodlite.Object()
)

// vercelSessionURL is base-client.js extractSessionId.
var vercelSessionURL = regexp.MustCompile(`/v2/sandboxes/sessions/([^/?]+)`)

// vercelResponseIssues validates a plain-parsed response body (base-client
// parse(): JSON.parse, then validator.safeParse) and returns zod's error
// message (JSON.stringify(issues, null, 2)), or "" when it matches.
func vercelResponseIssues(schema zodlite.Node, body []byte) string {
	message := zodlite.ValidationMessagePlain(schema, string(body))
	if message == "" {
		return ""
	}
	return zodlite.IssueText(message)
}

// vercelInvalidResponse is the APIError parse() returns for a response that
// fails its schema: "Response JSON is not valid: <ZodError>".
func vercelInvalidResponse(status int, path string, issues string) *APIError {
	err := &APIError{StatusCode: status, Message: "Response JSON is not valid: " + issues}
	if m := vercelSessionURL.FindStringSubmatch(path); m != nil {
		err.SessionID = m[1]
	}
	return err
}

// vercelLogLineStreams are the LogLine discriminator values in schema order.
var vercelLogLineStreams = []string{"stdout", "stderr", "error"}

// vercelLogLine is LogLine: z.discriminatedUnion("stream", [stdout, stderr,
// error]) as zod 4 reports it.
var vercelLogLine = func() zodlite.Node {
	text := zodlite.Object(zodlite.F("data", zStr))
	variants := map[string]zodlite.Node{
		"stdout": text,
		"stderr": text,
		"error": zodlite.Object(zodlite.F("data", zodlite.Object(
			zodlite.F("code", zStr),
			zodlite.F("message", zStr),
		))),
	}
	// zodlite's discriminated union keys on "type"; it still renders the
	// non-object issue zod gives any discriminated union.
	nonObject := zodlite.DiscriminatedUnion(vercelLogLineStreams, nil)
	return func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nonObject(v, present, path)
		}
		stream, _ := m["stream"].(string)
		n, known := variants[stream]
		if !known {
			return &zodlite.Issue{
				Path: append(append([]interface{}{}, path...), "stream"), Code: "invalid_union",
				Discriminator: "stream", Options: vercelLogLineStreams,
				Message: "Invalid discriminator value. Expected 'stdout' | 'stderr' | 'error'",
			}
		}
		return n(v, present, path)
	}
}()
