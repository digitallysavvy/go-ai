# WG3 handoff: harness bridge host transport (P1-8)

Worktree branch: `worktree-agent-af424fb8cc444540e`, based on `sep23-integration` @ aa4d7d0 (after `git reset --hard sep23-integration`).

## Done
- `f3963bd` feat(harness): bridge host transport. **Code only, no tests yet.** `go build ./pkg/harness/...` and `go vet ./pkg/harness/bridge/` pass.
  New files in `pkg/harness/bridge/`:
  - `token.go`: `CreateBridgeToken` (32-byte hex) and `WithBridgeToken(endpoint, token)`. It follows WHATWG `URLSearchParams.set`: order is preserved, the first match is replaced in place, and the value is appended when missing. Encoding is form-style (space→`+`; `*-._` left unescaped). Go's `url.Values.Encode` sorts keys, so it cannot be used here. (371e954)
  - `diagnostics.go`: `LineTail` (a mutex-guarded tail, because forwarders run on goroutines), `FormatBridgeError`, `LogBridgeError`, `CreateBridgeErrorHandler`, `CreateBridgeStartupError` (250ms stderr wait and 250ms exit-code wait), `ForwardBridgeProcessStream` and `DrainBridgeProcessStream` (both return a done chan), and `lineDecoder`. (39c8276)
  - `ready.go`: `MarkBridgeStarting`, `WaitForBridgeReady` (reads the stdout `bridge-ready` line; falls back to polling `bridge-meta.json` for `state=="waiting"` with a matching type and port > 0), `BridgeMetaPath`. On timeout or abort it kills the process. On EOF it returns the exit error. As in TS, lines flushed at EOF only feed the tail and are not parsed. Once it returns, the stdout goroutine keeps draining. (aae0138)
  - `conn.go`: `Conn` interface (Receive/Send/Close), `CloseError{Code, Reason}` (x/net cannot see close codes, so EOF maps to 1000 and anything else to 1006 "socket error"), and `Dial(ctx, endpoint, DialOptions{WaitForHello, HelloTimeout, HelloDeadline, OpenTimeout, OnHello, WriteTimeout, Name})`. `Dial` uses `websocket.Config.DialContext`, puts write deadlines on each Send, and handles abort through `context.AfterFunc` → `SetReadDeadline(now)`. Also: `OpenBridgeWebSocket` (the claude-code retry loop: 250ms*attempt capped at 1s; per attempt open ≤ min(10s, remaining) and hello ≤ min(5s, remaining); error text matches TS), `Sleep(ctx, d)` (d975097), `NewConnectFunc`, and `Hello.SupportsUserMessageResponses()`. (5cc654f, ab46c30)
  - `channel.go`: `Channel`, the port of TS `SandboxChannel` (7115a3f, c0e5d1d, d975097, ea3063f; no interrupt()). API: `NewChannel(ChannelOptions{Connect, Decode, Reconnect{MaxElapsed 30s, InitialDelay 50ms, MaxDelay 2s}, OnDebug, OnDiagnostic, OnBridgeError, InitialLastSeenEventID, SelectiveFlushGrace})`, `Open(ctx, resume)`, `On(type, func(Event)) unsubscribe`, `BeginListenerAttachment()`, `OnClose`, `OnReconnect`, `Send(InboundCommand)`, `BeginClose`, `Close`, `Suspend() <-chan float64`, `IsClosed`, `Done`, `LastSeenEventID`, `Event.PinCheckpoint()` / `PinEventCheckpoint`.
  - `user_message.go`: `ExperimentalUserMessageSubmitter` (`Submit(ctx, text)` blocks until the ack arrives; pending messages are resent with the same id on reconnect; `Close(err)`) and `NewChannelUserMessageSubmitter(ch)`. (eace6fb transport part)
  - `launcher.go`: `Launch(ctx, LaunchOptions)` runs mark-starting → spawn with `BRIDGE_CHANNEL_TOKEN`/`BRIDGE_WS_PORT`/`BRIDGE_REPLAY_FROM_DISK` → forward stderr → wait ready → resolve endpoint → `WithBridgeToken`. Also `ResolveBridgePort`, `ResolveBridgeEndpoint`, `BridgeEnvironment`, `ErrNoPort`, `ErrNoPortEndpoint`.
  - `testdata/runbridge/*.ndjson`: frames **recorded from the real TS `runBridge`** (see below): `ready`, `live` (hello, seq 1..4), `resume` (hello state=running lastSeq=4, replay from seq 4, a `user-message-response` accepted **with seq 6**, finish, a rejected user-message-response without seq, `bridge-stop`, close 1000 "stop"), `unauthorized` (close 1008 "unauthorized"). The `{"_close":…}` lines are recorder annotations, not frames.

## In progress: fixture recorder
- `pkg/harness/bridge/testdata/runbridge/record_runbridge.mts` drives the unmodified TS `packages/harness/src/bridge/index.ts` (runBridge) with a scripted turn, connects with `ws`, and prints every frame it receives. Regenerate as follows:
  1. In a scratch dir, run `npm init -y && npm pkg set type=module && npm i ws@8`.
  2. Copy `ai/packages/harness/src/bridge/index.ts` there as `bridge.ts`, together with `harness-bridge-capability-unsupported-error.ts`.
  3. In `bridge.ts`, change that import to use the `.ts` extension.
  4. Copy the script in and run `node record.mts > rec.out` (Node 24 strips types).
  5. Split `rec.out` on the `### <name>` headers into `<name>.ndjson`, and set the port in `ready.ndjson` to 4319.
- Left to do: add that regeneration header comment to the script. The fixtures themselves are complete.

## Remaining WG3 work
1. **Fake bridge server** `pkg/harness/bridge/bridgetest`: an httptest server using `websocket.Server` (x/net) with a Handshake that checks `agent_bridge_token`. It must match the recorded fixtures:
   - With a bad token, accept and then close with 1008. x/net has no WriteClose, so write the close frame manually or just close the connection.
   - Send hello `{state,lastSeq,capabilities:{experimental_userMessageResponses:true}}` without a seq.
   - Stamp a monotonic seq on events and keep the log.
   - Handle `resume` by replaying seq > n and claiming the socket.
   - Answer `user-message` with a `user-message-response`.
   - Answer `stop` with `bridge-stop{data}` and close.
   - Support hooks for dropping connections and delaying the hello.
   - Also provide a fake `SandboxSession`/`SandboxProcess` whose stdout prints the ready line and whose `GetPortEndpoint` returns the httptest URL.
2. **Tests** (none exist yet). Port these:
   - `sandbox-channel.test.ts` (all ~30 cases). Use an in-memory fake `Conn` like the TS fake socket. For the "across a microtask" case, register listeners back to back; that falls inside `SelectiveFlushGrace`.
   - `bridge-ready.test.ts` (5 cases, including the poll-interval test).
   - `bridge-token.test.ts` (3 cases; the expected URL is `...?existing=value&agent_bridge_token=token+with+special+characters+%26%3F`).
   - `bridge-user-message-submitter.test.ts` (4 cases).
   - `sleep.test.ts` (2 cases).
   - Diagnostics formatting and startup-error format tests.
   - A `Dial` hello-race test (5cc654f: hello sent immediately on accept), a hello timeout, a startup-timeout cleanup test (ab46c30: no goroutine leak, socket closed), and an abort test.
   - An NDJSON fixture replay: decode every recorded frame with `DecodeOutbound` and feed `live` and `resume` through a Channel; assert the cursor and ordering, and check that the user-message-response with seq advances the cursor.
   - A port of `reconnect.integration.test.ts` (both cases) against the fake server.
   - A `Launch` test with a fake sandbox, covering the ready line, the metadata fallback, and the exit/timeout error text: "`<label>` exited before becoming ready. Exit code: N.\n\nstdout:\n…\n\nstderr:\n…".
3. Run `gofmt`, `go vet ./pkg/harness/...`, `go test -race ./pkg/harness/...`, then `go test ./pkg/...` once. Commit with the Co-Authored-By line from the instructions.
4. Final report tracking rows (per `state/parity/sep_23_2026/IMPL_INSTRUCTIONS.md`): d975097, c0e5d1d, 371e954, fc3baaf, 7115a3f, ab46c30, 5cc654f, ea3063f, 39c8276, aae0138, plus the eace6fb transport part.
   - fc3baaf: `harness.MintBridgeTokenCallback func(sandboxID string) string` already exists in `spec.go` (WG1). Launch takes `Token`, so the adapter passes either `Mint(id)` or `CreateBridgeToken()`. Mark it Implemented at the transport level; per-adapter settings belong to WG7+.

## Key TS files (read from `/Users/arlene/Dev/side-projects/go-ai/ai`; worktree git access to it is blocked, but the working tree equals the tag for these files)
- `packages/harness/src/utils/{sandbox-channel,bridge-ready,bridge-token,bridge-diagnostics,bridge-user-message-submitter,sleep}.ts` and their `.test.ts` files
- `packages/harness/src/bridge/index.ts` (runBridge: hello at line ~1001, token check ~992, replay ~598, user-message ~890) and `bridge/reconnect.integration.test.ts`
- `packages/harness-claude-code/src/claude-code-harness.ts` lines 820–1490 (launch sequence, `openWebSocketAndWaitForBridgeHello`, `openBridgeWebSocket`); codex, acp and deepagents use a plain `openWebSocket` without a hello wait (`DialOptions{WaitForHello:false}`)

## Gotchas and decisions
- **No event loop in Go.** The TS "selective flush after the current task" is replaced by a debounce timer, `SelectiveFlushGrace` (5ms by default), that is re-armed on every `On`. For strict ordering across several registrations, use `BeginListenerAttachment` (TS adapters already do this for replay/continue).
- Delivery is single-flight through the `flushing` flag with request flags (`reqTypes`, `reqOrdered`, `reqSelective`). Listeners never run concurrently, but they can run on the dispatch goroutine, the goroutine calling `On`, or the timer goroutine. A re-entrant `On` from inside a listener is safe (the request is recorded and the active deliverer picks it up).
- Inbound processing runs on an on-demand FIFO work queue (`enqueue`/`runQueue`), which mirrors the TS `dispatchChain`. The reconnect loop also runs as a queue item, so later frames wait for it, as in TS.
- The ctx passed to `ConnectFunc` is **canceled after it returns**; it only scopes connection establishment. This avoids leaking child contexts.
- Divergences from TS, both deliberate:
  - `Open` also flushes `pendingSends`; TS only flushes them on reconnect.
  - A failed `Send` requeues the frame and closes the socket so the reconnect resends it; TS `ws` silently drops it.
- `Suspend()` returns a chan instead of blocking, so it can be called from a listener without deadlocking the dispatch goroutine.
- A malformed frame is dispatched as `StreamPartFrame{&harness.ErrorPart{Error: err}}` and also goes to `OnBridgeError`.
- Diagnostic frames (`sandbox-log`, `debug-event`) go only to `OnDiagnostic`, but they still advance the cursor.
- The fix in 5cc654f (the hello frame was missed) is inherent in Go: `Dial` reads synchronously until the hello arrives and ignores frames that come before it.
- `DecodeOutbound` already returns seq on the stream-part path; `UserMessageResponse` frames can carry a seq (as the fixture shows).

## Status
Build and vet are clean at f3963bd. No tests have been written or run yet for the new files. Pre-existing packages were not touched.
