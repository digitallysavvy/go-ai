// ../harness/dist/bridge/index.js
import { appendFile, mkdir, writeFile } from "fs/promises";
import { existsSync, readFileSync } from "fs";
import { randomUUID } from "crypto";
import { env as procEnv, pid, stdout } from "process";
import { WebSocketServer } from "ws";
var DEBUG_LEVEL_WEIGHT = {
  error: 0,
  warn: 1,
  info: 2,
  debug: 3,
  trace: 4
};
function subsystemMatches(filters, subsystem) {
  if (!filters || filters.length === 0) return true;
  return filters.some(
    (filter) => subsystem === filter || subsystem.startsWith(`${filter}.`)
  );
}
function formatBridgeError(err) {
  if (err instanceof Error) {
    return { name: err.name, message: err.message, stack: err.stack };
  }
  if (typeof err === "string") {
    return { message: err };
  }
  if (err !== null && typeof err === "object") {
    try {
      return { message: JSON.stringify(err) };
    } catch {
    }
  }
  return { message: String(err) };
}
function createBridgeUserMessageQueue(options) {
  const messages = [];
  const waiters = [];
  const entries = /* @__PURE__ */ new Map();
  let closed = false;
  let pendingCount = 0;
  const enqueue = (input) => {
    const existing = entries.get(input.messageId);
    if (existing != null) {
      if (existing.response != null) {
        options.respond(existing.response);
      }
      return;
    }
    let settled = false;
    const settle = (response) => {
      if (settled) return;
      settled = true;
      pendingCount--;
      const entry = entries.get(input.messageId);
      if (entry != null) entry.response = response;
      options.respond(response);
    };
    const message = {
      messageId: input.messageId,
      text: input.text,
      accept: () => {
        settle({
          type: "user-message-response",
          messageId: input.messageId,
          accepted: true
        });
      },
      reject: (error) => {
        settle({
          type: "user-message-response",
          messageId: input.messageId,
          accepted: false,
          error: { message: formatBridgeError(error).message }
        });
      }
    };
    entries.set(input.messageId, {
      reject: message.reject
    });
    pendingCount++;
    if (closed) {
      message.reject(
        new Error("The bridge turn is no longer accepting user messages.")
      );
      return;
    }
    const waiter = waiters.shift();
    if (waiter != null) {
      waiter({ done: false, value: message });
    } else {
      messages.push(message);
    }
  };
  const close = (error) => {
    if (closed) return;
    closed = true;
    const reason = error ?? new Error("The bridge turn ended before accepting the user message.");
    for (const entry of entries.values()) {
      if (entry.response == null) entry.reject(reason);
    }
    messages.length = 0;
    while (waiters.length > 0) {
      waiters.shift()({ done: true, value: void 0 });
    }
  };
  return {
    get pendingCount() {
      return pendingCount;
    },
    enqueue,
    close,
    [Symbol.asyncIterator]() {
      return {
        next: () => {
          const message = messages.shift();
          if (message != null) {
            return Promise.resolve({ done: false, value: message });
          }
          if (closed) {
            return Promise.resolve({
              done: true,
              value: void 0
            });
          }
          return new Promise(
            (resolve) => {
              waiters.push(resolve);
            }
          );
        }
      };
    }
  };
}
function parseEnvList(value) {
  if (!value) return void 0;
  const items = value.split(",").map((item) => item.trim()).filter(Boolean);
  return items.length > 0 ? items : void 0;
}
var ENV_TRUTHY = /* @__PURE__ */ new Set(["1", "true", "yes", "on"]);
var WS_OPEN = 1;
async function runBridge(options) {
  const { bridgeType, bridgeStateDir: bridgeStateDir2, onStart, onStop, onDestroy } = options;
  const teardownGraceMs = options.turnTeardownGraceMs ?? 1e4;
  const expectedToken = options.token ?? procEnv.BRIDGE_CHANNEL_TOKEN ?? "";
  const bridgeWsPort = options.port ?? parseInt(procEnv.BRIDGE_WS_PORT ?? "0", 10);
  const bridgeMetaPath = `${bridgeStateDir2}/bridge-meta.json`;
  const startConfigPath = `${bridgeStateDir2}/start-config.json`;
  const rerunStartConfigPath = `${bridgeStateDir2}/rerun-start-config.json`;
  const eventLogPath = `${bridgeStateDir2}/event-log.ndjson`;
  try {
    await mkdir(bridgeStateDir2, { recursive: true });
  } catch {
  }
  let currentBoundPort = 0;
  let currentTurnState = "init";
  let activeSocket;
  let isFirstTurn = true;
  let turnAbort;
  let currentUserMessages;
  let activeTurn;
  let debugConfig;
  let consoleCaptureInstalled = false;
  const envDebugEnabled = ENV_TRUTHY.has(
    (procEnv.HARNESS_DEBUG ?? "").toLowerCase()
  );
  let seqCounter = 0;
  let eventLog = [];
  let diskBuffer = "";
  let flushPromise = null;
  const flushEventsToDisk = async () => {
    while (diskBuffer.length > 0) {
      const buf = diskBuffer;
      diskBuffer = "";
      await appendFile(eventLogPath, buf).catch(() => {
      });
    }
  };
  const scheduleEventFlush = () => {
    if (flushPromise) return;
    flushPromise = new Promise((resolve) => {
      setImmediate(() => {
        void flushEventsToDisk().finally(resolve);
      });
    }).finally(() => {
      flushPromise = null;
      if (diskBuffer.length > 0) {
        scheduleEventFlush();
      }
    });
  };
  const flushPendingEventsToDisk = async () => {
    if (diskBuffer.length > 0 && !flushPromise) {
      scheduleEventFlush();
    }
    let inFlight = flushPromise;
    while (inFlight) {
      await inFlight;
      inFlight = flushPromise;
    }
  };
  const replayFromDisk = procEnv.BRIDGE_REPLAY_FROM_DISK === "1";
  if (replayFromDisk && existsSync(eventLogPath)) {
    try {
      const lines = readFileSync(eventLogPath, "utf8").split("\n").map((line) => line.trim()).filter(Boolean);
      eventLog = lines.map((line) => ({
        seq: JSON.parse(line).seq,
        line
      }));
      seqCounter = eventLog.at(-1)?.seq ?? 0;
    } catch {
      eventLog = [];
      seqCounter = 0;
    }
  }
  const pendingToolResults = /* @__PURE__ */ new Map();
  const bufferedToolResults = [];
  const pendingToolApprovals = /* @__PURE__ */ new Map();
  const writeBridgeMeta = async (state) => {
    try {
      await writeFile(
        bridgeMetaPath,
        JSON.stringify({
          type: bridgeType,
          port: currentBoundPort,
          state,
          pid
        })
      );
    } catch {
    }
  };
  const writeStartConfig = async (start) => {
    try {
      const serialized = JSON.stringify(start);
      await writeFile(startConfigPath, serialized);
      if (!existsSync(rerunStartConfigPath)) {
        await writeFile(rerunStartConfigPath, serialized);
      }
    } catch {
    }
  };
  const emit = (event) => {
    const seq = ++seqCounter;
    const line = JSON.stringify({ ...event, seq });
    eventLog.push({ seq, line });
    diskBuffer += `${line}
`;
    scheduleEventFlush();
    if (activeSocket?.readyState === WS_OPEN) {
      try {
        activeSocket.send(line);
      } catch {
      }
    }
  };
  const replay = (ws, afterSeq) => {
    for (const entry of eventLog) {
      if (entry.seq > afterSeq && ws.readyState === WS_OPEN) {
        ws.send(entry.line);
      }
    }
  };
  const shouldEmitDebugEvent = (level, subsystem) => {
    if (!debugConfig?.enabled) return false;
    const threshold = debugConfig.level ?? "debug";
    if (DEBUG_LEVEL_WEIGHT[level] > DEBUG_LEVEL_WEIGHT[threshold]) return false;
    return subsystemMatches(debugConfig.subsystems, subsystem);
  };
  const rawStdoutWrite = process.stdout.write.bind(process.stdout);
  const rawStderrWrite = process.stderr.write.bind(process.stderr);
  const writeErrorToStderr = (input) => {
    try {
      const formatted = formatBridgeError(input.error);
      rawStderrWrite(
        `[harness:${bridgeType}:error] ${input.message}: ${formatted.message}
`
      );
      if (formatted.stack) {
        rawStderrWrite(`${formatted.stack}
`);
      }
    } catch {
    }
  };
  const emitWarning = (input) => {
    try {
      for (const line of input.message.split("\n")) {
        if (line.trim().length > 0) {
          rawStderrWrite(`[harness:${bridgeType}:warn] ${line}
`);
        }
      }
    } catch {
    }
  };
  const emitError = (input) => {
    writeErrorToStderr({
      message: input.message ?? "bridge error",
      error: input.error
    });
    emit({ type: "error", error: serialiseError(input.error) });
  };
  const installConsoleCapture = () => {
    if (consoleCaptureInstalled) return;
    consoleCaptureInstalled = true;
    const buffers = {
      stdout: "",
      stderr: ""
    };
    const patch = (stream, raw) => (chunk, encoding, cb) => {
      if (debugConfig?.enabled) {
        try {
          const enc = typeof encoding === "string" ? encoding : "utf8";
          const text = typeof chunk === "string" ? chunk : Buffer.from(chunk).toString(
            enc
          );
          const combined = buffers[stream] + text.replace(/\r\n/g, "\n");
          const parts = combined.split("\n");
          buffers[stream] = parts.pop() ?? "";
          for (const line of parts) {
            const trimmed = line.replace(/\s+$/, "");
            if (trimmed) {
              emit({
                type: "sandbox-log",
                source: bridgeType,
                stream,
                line: trimmed
              });
            }
          }
        } catch {
        }
      }
      return raw(
        chunk,
        encoding,
        cb
      );
    };
    process.stdout.write = patch(
      "stdout",
      rawStdoutWrite
    );
    process.stderr.write = patch(
      "stderr",
      rawStderrWrite
    );
  };
  const handleInbound = async (msg, ws) => {
    switch (msg.type) {
      case "start": {
        for (; ; ) {
          const pendingTurn = activeTurn;
          if (pendingTurn == null) break;
          turnAbort?.abort();
          currentUserMessages?.close(
            new Error("A new bridge turn replaced the active turn.")
          );
          let graceTimer;
          const settled = await Promise.race([
            pendingTurn.then(() => true),
            new Promise((resolve) => {
              graceTimer = setTimeout(() => resolve(false), teardownGraceMs);
              graceTimer.unref?.();
            })
          ]);
          clearTimeout(graceTimer);
          if (!settled) break;
        }
        let turnFinished;
        const thisTurn = new Promise((resolve) => turnFinished = resolve);
        activeTurn = thisTurn;
        activeSocket = ws;
        const firstTurn = isFirstTurn;
        isFirstTurn = false;
        eventLog = [];
        diskBuffer = "";
        void writeFile(eventLogPath, "").catch(() => {
        });
        turnAbort = new AbortController();
        currentTurnState = "running";
        void writeStartConfig(msg);
        void writeBridgeMeta("running");
        const startDebug = msg.debug;
        debugConfig = {
          enabled: startDebug?.enabled ?? envDebugEnabled,
          level: startDebug?.level ?? procEnv.HARNESS_DEBUG_LEVEL,
          subsystems: startDebug?.subsystems ?? parseEnvList(procEnv.HARNESS_DEBUG_SUBSYSTEMS)
        };
        if (debugConfig.enabled) {
          installConsoleCapture();
        }
        const userMessages = createBridgeUserMessageQueue({ respond: emit });
        const turn = {
          emit,
          requestToolResult: (requestInput) => {
            const request = typeof requestInput === "string" ? { toolCallId: requestInput } : requestInput;
            const bufferedIndex = bufferedToolResults.findIndex(
              (buffered) => buffered.toolCallId === request.toolCallId || request.matches?.(buffered.result) === true
            );
            if (bufferedIndex >= 0) {
              return Promise.resolve(
                bufferedToolResults.splice(bufferedIndex, 1)[0].result
              );
            }
            return new Promise((resolve) => {
              pendingToolResults.set(request.toolCallId, {
                resolve,
                matches: request.matches
              });
            });
          },
          requestToolApproval: (approvalId) => new Promise((resolve) => {
            pendingToolApprovals.set(approvalId, resolve);
          }),
          experimental_userMessages: userMessages,
          abortSignal: turnAbort.signal,
          firstTurn,
          bridgeLog: (input) => {
            const level = input.level ?? "debug";
            if (!shouldEmitDebugEvent(level, input.subsystem)) return;
            emit({
              type: "debug-event",
              level,
              subsystem: input.subsystem,
              message: input.message,
              ...input.attrs ? { attrs: input.attrs } : {},
              ...input.error !== void 0 ? { error: formatBridgeError(input.error) } : {}
            });
          },
          emitWarning,
          emitError
        };
        currentUserMessages = userMessages;
        try {
          await onStart(msg, turn);
        } catch (err) {
          emitError({ error: err, message: "bridge turn failed" });
        } finally {
          userMessages.close();
          if (currentUserMessages === userMessages) {
            currentUserMessages = void 0;
          }
          if (activeTurn === thisTurn) {
            activeTurn = void 0;
            currentTurnState = "waiting";
            void writeBridgeMeta("waiting");
          }
          turnFinished();
        }
        return;
      }
      case "tool-result": {
        const result = {
          output: msg.output,
          isError: msg.isError,
          toolResult: msg.toolResult
        };
        const exactPending = pendingToolResults.get(msg.toolCallId);
        const matchingPending = exactPending == null ? Array.from(pendingToolResults.entries()).find(
          ([, pending2]) => pending2.matches?.(result) === true
        ) : void 0;
        const pending = exactPending ?? matchingPending?.[1];
        const pendingId = exactPending != null ? msg.toolCallId : matchingPending?.[0];
        if (pending != null && pendingId != null) {
          pendingToolResults.delete(pendingId);
          pending.resolve(result);
        } else {
          bufferedToolResults.push({
            toolCallId: msg.toolCallId,
            result
          });
        }
        return;
      }
      case "tool-approval-response": {
        const resolver = pendingToolApprovals.get(msg.approvalId);
        if (resolver) {
          pendingToolApprovals.delete(msg.approvalId);
          resolver({ approved: msg.approved, reason: msg.reason });
        }
        return;
      }
      case "user-message": {
        const messageId = msg.messageId ?? randomUUID();
        if (currentUserMessages == null) {
          sendControl(ws, {
            type: "user-message-response",
            messageId,
            accepted: false,
            error: { message: "The bridge has no active turn to steer." }
          });
          return;
        }
        if (ws !== activeSocket) {
          sendControl(ws, {
            type: "user-message-response",
            messageId,
            accepted: false,
            error: {
              message: "The connection does not own the active bridge turn."
            }
          });
          return;
        }
        currentUserMessages.enqueue({
          messageId,
          text: msg.text
        });
        return;
      }
      case "abort":
        turnAbort?.abort();
        return;
      case "resume":
        activeSocket = ws;
        replay(ws, msg.lastSeenEventId);
        return;
      case "destroy":
        currentTurnState = "done";
        void writeBridgeMeta("done");
        await onDestroy?.();
        drainThenExit(ws, 1e3, "destroy");
        return;
      case "stop": {
        currentTurnState = "done";
        void writeBridgeMeta("done");
        const data = await onStop?.() ?? {};
        sendControl(ws, { type: "bridge-stop", data });
        drainThenExit(ws, 1e3, "stop");
      }
    }
  };
  void writeBridgeMeta("init");
  const wss = new WebSocketServer({ port: bridgeWsPort, host: "0.0.0.0" });
  const exit = () => {
    if (options.onExit) {
      options.onExit();
      return;
    }
    wss.close(() => process.exit(0));
    setTimeout(() => process.exit(0), 1e3).unref();
  };
  const drainThenExit = (ws, code, reason) => {
    const start = Date.now();
    const tick = () => {
      const drained = ws.bufferedAmount === 0 || ws.readyState !== WS_OPEN;
      if (drained || Date.now() - start >= 5e3) {
        void flushPendingEventsToDisk().finally(() => {
          try {
            ws.close(code, reason);
          } finally {
            exit();
          }
        });
        return;
      }
      setTimeout(tick, 10).unref();
    };
    tick();
  };
  wss.on("listening", () => {
    const addr = wss.address();
    currentBoundPort = typeof addr === "object" && addr ? addr.port : 0;
    currentTurnState = "waiting";
    void writeBridgeMeta("waiting");
    stdout.write(
      JSON.stringify({
        type: "bridge-ready",
        port: currentBoundPort
      }) + "\n"
    );
    options.onListening?.(currentBoundPort);
  });
  wss.on("connection", (ws, req) => {
    const url = new URL(req.url ?? "/", "http://localhost");
    if (url.searchParams.get("agent_bridge_token") !== expectedToken) {
      ws.close(1008, "unauthorized");
      return;
    }
    sendControl(ws, {
      type: "bridge-hello",
      state: currentTurnState,
      lastSeq: seqCounter,
      capabilities: { experimental_userMessageResponses: true }
    });
    ws.on("message", (raw) => {
      let parsed;
      try {
        const text = typeof raw === "string" ? raw : Buffer.from(raw).toString("utf8");
        parsed = JSON.parse(text);
      } catch (err) {
        sendControl(ws, {
          type: "error",
          error: `protocol parse error: ${err.message}`
        });
        return;
      }
      void handleInbound(parsed, ws);
    });
    ws.on("close", () => {
      if (activeSocket === ws) {
        activeSocket = void 0;
      }
    });
    ws.on("error", () => {
    });
  });
  process.on("uncaughtException", (err) => {
    emitError({ error: err, message: "uncaught exception" });
  });
  process.on("unhandledRejection", (err) => {
    emitError({ error: err, message: "unhandled rejection" });
  });
  await new Promise((resolve, reject) => {
    if (wss.address() != null) {
      resolve();
      return;
    }
    wss.once("listening", resolve);
    wss.once("error", reject);
  });
  return {
    port: currentBoundPort,
    close: () => new Promise((resolve) => {
      wss.close(() => resolve());
    })
  };
}
function sendControl(socket, message) {
  if (socket?.readyState === WS_OPEN) {
    try {
      socket.send(JSON.stringify(message));
    } catch {
    }
  }
}
function serialiseError(err) {
  if (err instanceof Error) {
    return { name: err.name, message: err.message, stack: err.stack };
  }
  return err;
}

// src/bridge/compaction-latch.ts
function createCompactionLatch(emit) {
  let boundary;
  let summary;
  const tryEmit = () => {
    if (!boundary || summary === void 0) return;
    emit({
      type: "compaction",
      trigger: boundary.trigger,
      summary,
      ...boundary.tokensBefore !== void 0 ? { tokensBefore: boundary.tokensBefore } : {},
      ...boundary.tokensAfter !== void 0 ? { tokensAfter: boundary.tokensAfter } : {}
    });
    boundary = void 0;
    summary = void 0;
  };
  return {
    onBoundary(next) {
      boundary = next;
      tryEmit();
    },
    onSummary(next) {
      summary = next;
      tryEmit();
    }
  };
}

// src/bridge/index.ts
import { randomUUID as randomUUID3 } from "crypto";
import { argv, env as procEnv2, stdout as stdout2 } from "process";
import * as claudeAgentSdk from "@anthropic-ai/claude-agent-sdk";
import * as mcpServerModule from "@modelcontextprotocol/sdk/server/mcp.js";

// src/bridge/claude-code-system-prompt.ts
function createClaudeCodeSystemPrompt(instructions) {
  return {
    type: "preset",
    preset: "claude_code",
    ...instructions ? { append: instructions } : {}
  };
}

// src/bridge/claude-skills-option.ts
function toClaudeSkillsOption(skills) {
  return skills && skills.length > 0 ? "all" : void 0;
}

// src/bridge/create-emit-stream-event.ts
import { randomUUID as randomUUID2 } from "crypto";
function createClaudeStreamEventState() {
  return {
    nativeToolCallNames: /* @__PURE__ */ new Map(),
    approvalRequestedToolUseIds: /* @__PURE__ */ new Set(),
    partialBlocks: /* @__PURE__ */ new Map(),
    stepUsage: void 0,
    pendingStepToolUseIds: /* @__PURE__ */ new Set(),
    pendingStepAssistantUsage: void 0,
    pendingStepDeltaUsage: void 0,
    pendingStepUsage: void 0,
    stepOpen: false,
    mcpToolUseIds: /* @__PURE__ */ new Set(),
    externalMcpToolUseIds: /* @__PURE__ */ new Set(),
    structuredOutputToolUseIds: /* @__PURE__ */ new Set(),
    observedTerminalError: void 0
  };
}
var UNRECOVERABLE_API_RETRY_STATUSES = /* @__PURE__ */ new Set([401, 403, 404]);
var HOST_TOOL_PREFIX = "mcp__harness-tools__";
function isExternalMcpTool(nativeName) {
  return nativeName.startsWith("mcp__") && !nativeName.startsWith(HOST_TOOL_PREFIX);
}
function createEmitStreamEvent({
  state,
  emit,
  emitWarning,
  emitTerminalError,
  onCompactionBoundary,
  toCommonName: toCommonName2
}) {
  let streamStarted = false;
  return (msg) => {
    const type = msg.type;
    if (!streamStarted) {
      const initModel = type === "system" && msg.subtype === "init" && typeof msg.model === "string" ? msg.model : void 0;
      emit({
        type: "stream-start",
        ...initModel ? { modelId: initModel } : {}
      });
      streamStarted = true;
    }
    if (type === "system" && msg.subtype === "api_retry") {
      if (typeof msg.error_status === "number" && UNRECOVERABLE_API_RETRY_STATUSES.has(msg.error_status)) {
        emitTerminalError(
          `HTTP ${msg.error_status}: ${msg.error ?? "provider request failed"}`
        );
        return;
      }
      emitWarning({ message: formatApiRetryWarning(msg) });
      return;
    }
    if (typeof msg.error === "string" && msg.error.trim()) {
      state.observedTerminalError = msg.error.trim();
    }
    if (type === "auth_status" && typeof msg.error === "string" && msg.error.trim()) {
      emitTerminalError(msg.error);
      return;
    }
    if (type === "system" && msg.subtype === "task_updated" && msg.patch?.status === "failed" && typeof msg.patch.error === "string") {
      emitTerminalError(msg.patch.error);
      return;
    }
    if (type === "system" && msg.subtype === "compact_boundary") {
      const meta = msg.compact_metadata;
      if (meta) {
        onCompactionBoundary({
          trigger: meta.trigger,
          ...typeof meta.pre_tokens === "number" ? { tokensBefore: meta.pre_tokens } : {},
          ...typeof meta.post_tokens === "number" ? { tokensAfter: meta.post_tokens } : {}
        });
      }
      return;
    }
    if (msg.parent_tool_use_id != null) {
      return;
    }
    if (type === "stream_event") {
      handleStreamEvent({
        event: msg.event,
        state,
        send: emit,
        toCommonName: toCommonName2
      });
      return;
    }
    const messageContent = msg.message?.content;
    if (type === "assistant" && Array.isArray(messageContent)) {
      const usage = toUsageRecord(msg.message?.usage);
      const toolUseIds = [];
      let opensStep = false;
      for (const block of messageContent) {
        if (block.type === "tool_use" && typeof block.id === "string" && typeof block.name === "string") {
          toolUseIds.push(block.id);
          if (block.name === "StructuredOutput") {
            state.structuredOutputToolUseIds.add(block.id);
            continue;
          }
          if (block.name.startsWith(HOST_TOOL_PREFIX)) {
            state.pendingStepToolUseIds.add(block.id);
            state.mcpToolUseIds.add(block.id);
            opensStep = true;
            continue;
          }
          state.nativeToolCallNames.set(block.id, block.name);
          if (block.name === "AskUserQuestion") {
            state.pendingStepToolUseIds.add(block.id);
            opensStep = true;
            continue;
          }
          const dynamic = isExternalMcpTool(block.name);
          if (dynamic) state.externalMcpToolUseIds.add(block.id);
          if (state.approvalRequestedToolUseIds.has(block.id)) {
            continue;
          }
          state.pendingStepToolUseIds.add(block.id);
          opensStep = true;
          emit({
            type: "tool-call",
            toolCallId: block.id,
            toolName: toCommonName2(block.name),
            nativeName: block.name,
            input: JSON.stringify(block.input ?? {}),
            providerExecuted: true,
            ...dynamic ? { dynamic: true } : {}
          });
        }
      }
      if (opensStep || toolUseIds.length === 0) {
        state.stepOpen = true;
        if (usage) {
          state.pendingStepAssistantUsage = usage;
          updatePendingStepUsage(state);
        }
      }
      return;
    }
    if (type === "user" && Array.isArray(messageContent)) {
      const toolResultBlocks = messageContent.filter(
        (block) => block.type === "tool_result"
      );
      const toolUseResult = toolResultBlocks.length === 1 ? msg.tool_use_result : void 0;
      for (const block of messageContent) {
        if (block.type === "tool_result" && typeof block.tool_use_id === "string") {
          if (state.structuredOutputToolUseIds.delete(block.tool_use_id)) {
            continue;
          }
          if (state.mcpToolUseIds.has(block.tool_use_id)) {
            state.mcpToolUseIds.delete(block.tool_use_id);
            state.pendingStepToolUseIds.delete(block.tool_use_id);
            continue;
          }
          state.approvalRequestedToolUseIds.delete(block.tool_use_id);
          const nativeName = state.nativeToolCallNames.get(block.tool_use_id) ?? "unknown";
          state.nativeToolCallNames.delete(block.tool_use_id);
          const toolName = toCommonName2(nativeName);
          const dynamic = state.externalMcpToolUseIds.delete(block.tool_use_id);
          const isError = !!block.is_error;
          const result = toolUseResult !== void 0 ? toolUseResult : resolveToolResult({
            toolName,
            dynamic,
            isError,
            rawContent: block.content
          });
          emit({
            type: "tool-result",
            toolCallId: block.tool_use_id,
            toolName,
            result,
            isError,
            ...dynamic ? { dynamic: true } : {}
          });
          state.pendingStepToolUseIds.delete(block.tool_use_id);
        }
      }
      closeStepIfReady({ state, emit });
    }
  };
}
function finishApprovalStep({
  state,
  emit,
  approvalId
}) {
  state.stepOpen = true;
  state.pendingStepToolUseIds.delete(approvalId);
  closeStepIfReady({ state, emit });
}
function emitFinishStep({
  state,
  emit,
  usage
}) {
  emit({
    type: "finish-step",
    finishReason: { unified: "stop", raw: "stop" },
    usage: usage ?? defaultUsage()
  });
  state.stepUsage = usage ?? state.stepUsage;
  state.pendingStepAssistantUsage = void 0;
  state.pendingStepDeltaUsage = void 0;
  state.pendingStepUsage = void 0;
  state.pendingStepToolUseIds = /* @__PURE__ */ new Set();
  state.stepOpen = false;
}
function closeStepIfReady({
  state,
  emit
}) {
  if (!state.stepOpen || state.pendingStepToolUseIds.size > 0 || state.partialBlocks.size > 0) {
    return;
  }
  emitFinishStep({ state, emit, usage: state.pendingStepUsage });
}
function formatApiRetryWarning(msg) {
  const details = [];
  if (typeof msg.attempt === "number") {
    const maxRetries = typeof msg.max_retries === "number" ? `/${msg.max_retries}` : "";
    details.push(`attempt ${msg.attempt}${maxRetries}`);
  }
  if (typeof msg.error_status === "number") {
    details.push(`HTTP ${msg.error_status}`);
  }
  if (typeof msg.retry_delay_ms === "number") {
    details.push(`retrying in ${msg.retry_delay_ms}ms`);
  }
  if (msg.error) details.push(msg.error);
  return details.length > 0 ? `Claude Code API retry: ${details.join("; ")}` : "Claude Code API retry";
}
function handleStreamEvent({
  event,
  state,
  send,
  toCommonName: toCommonName2
}) {
  if (!event) return;
  if (event.type === "message_delta") {
    const usage = toUsageRecord(event.usage);
    if (usage) {
      state.pendingStepDeltaUsage = mergeNonNullUsage(
        state.pendingStepDeltaUsage,
        usage
      );
      updatePendingStepUsage(state);
    }
    return;
  }
  if (typeof event.index !== "number") return;
  const index = event.index;
  const partialBlocks = state.partialBlocks;
  if (event.type === "content_block_start") {
    const blockType = event.content_block?.type;
    if (blockType === "text") {
      const id = randomUUID2();
      partialBlocks.set(index, { id, kind: "text" });
      send({ type: "text-start", id });
    } else if (blockType === "thinking") {
      const id = randomUUID2();
      partialBlocks.set(index, { id, kind: "thinking" });
      send({ type: "reasoning-start", id });
    } else if (blockType === "tool_use" && typeof event.content_block?.id === "string" && typeof event.content_block.name === "string") {
      const id = event.content_block.id;
      const nativeName = event.content_block.name;
      if (nativeName === "StructuredOutput") {
        return;
      }
      if (nativeName === "AskUserQuestion") {
        return;
      }
      const hostToolName = nativeName.startsWith(HOST_TOOL_PREFIX) ? nativeName.slice(HOST_TOOL_PREFIX.length) : void 0;
      const dynamic = isExternalMcpTool(nativeName);
      partialBlocks.set(index, { id, kind: "tool-input" });
      send({
        type: "tool-input-start",
        id,
        toolName: hostToolName ?? toCommonName2(nativeName),
        providerExecuted: hostToolName === void 0,
        ...dynamic ? { dynamic: true } : {}
      });
    }
    return;
  }
  if (event.type === "content_block_delta") {
    const block = partialBlocks.get(index);
    if (!block) return;
    if (block.kind === "text" && event.delta?.type === "text_delta" && typeof event.delta.text === "string") {
      send({ type: "text-delta", id: block.id, delta: event.delta.text });
    } else if (block.kind === "thinking" && event.delta?.type === "thinking_delta" && typeof event.delta.thinking === "string") {
      send({
        type: "reasoning-delta",
        id: block.id,
        delta: event.delta.thinking
      });
    } else if (block.kind === "tool-input" && event.delta?.type === "input_json_delta" && typeof event.delta.partial_json === "string") {
      send({
        type: "tool-input-delta",
        id: block.id,
        delta: event.delta.partial_json
      });
    }
    return;
  }
  if (event.type === "content_block_stop") {
    const block = partialBlocks.get(index);
    if (!block) return;
    partialBlocks.delete(index);
    if (block.kind === "text") {
      send({ type: "text-end", id: block.id });
    } else if (block.kind === "thinking") {
      send({ type: "reasoning-end", id: block.id });
    } else {
      send({ type: "tool-input-end", id: block.id });
    }
  }
}
function toUsageRecord(usage) {
  return usage != null && typeof usage === "object" ? usage : void 0;
}
function mergeNonNullUsage(current, update) {
  const merged = { ...current };
  for (const [key, value] of Object.entries(update)) {
    if (value != null) {
      merged[key] = value;
    }
  }
  return merged;
}
function updatePendingStepUsage(state) {
  const assistantUsage = state.pendingStepAssistantUsage;
  const deltaUsage = state.pendingStepDeltaUsage;
  state.pendingStepUsage = mapUsage(
    assistantUsage || deltaUsage ? { ...assistantUsage, ...deltaUsage } : void 0
  );
}
function isTextEntry(entry) {
  return entry != null && typeof entry === "object" && "text" in entry;
}
function stringifyContent(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content.map(
      (entry) => isTextEntry(entry) ? String(entry.text ?? "") : JSON.stringify(entry)
    ).join("");
  }
  return JSON.stringify(content);
}
function hasNonTextContent(content) {
  return Array.isArray(content) && content.some((entry) => !isTextEntry(entry));
}
function resolveToolResult({
  toolName,
  dynamic,
  isError,
  rawContent
}) {
  if (toolName === "bash") {
    return { exitCode: isError ? 1 : 0, stdout: stringifyContent(rawContent) };
  }
  if (hasNonTextContent(rawContent)) return rawContent;
  const content = stringifyContent(rawContent);
  return dynamic ? parseMcpToolResult(content) : content;
}
function parseMcpToolResult(content) {
  try {
    const parsed = JSON.parse(content);
    return parsed !== null && typeof parsed === "object" ? parsed : content;
  } catch {
    return content;
  }
}
function mapUsage(usage) {
  if (!usage || typeof usage !== "object") return void 0;
  const u = usage;
  return {
    inputTokens: {
      total: (u.input_tokens ?? 0) + (u.cache_creation_input_tokens ?? 0) + (u.cache_read_input_tokens ?? 0),
      noCache: u.input_tokens ?? 0,
      cacheRead: u.cache_read_input_tokens ?? 0,
      cacheWrite: u.cache_creation_input_tokens ?? 0
    },
    outputTokens: {
      total: u.output_tokens ?? 0,
      text: u.output_tokens ?? 0
    }
  };
}
function defaultUsage() {
  return {
    inputTokens: { total: 0, noCache: 0, cacheRead: 0, cacheWrite: 0 },
    outputTokens: { total: 0, text: 0 }
  };
}

// src/bridge/json-schema-to-zod.ts
import { z } from "zod/v4";
function jsonSchemaToZodObject(input) {
  const schema = isJsonSchemaObject(input) ? input : {};
  return toZodObject(schema);
}
function toZodObject(schema) {
  const object = z.object(toZodShape(schema));
  if (schema.additionalProperties === false) return object.strict();
  return object.catchall(
    isJsonSchemaObject(schema.additionalProperties) ? toZodType(schema.additionalProperties) : z.unknown()
  );
}
function toZodShape(schema) {
  if (!schema?.properties) return {};
  const required = new Set(schema.required);
  const shape = {};
  for (const [key, propSchema] of Object.entries(schema.properties)) {
    const propType = toZodType(propSchema);
    let propertyType = required.has(key) ? propType : propType.optional();
    if (propSchema.description) {
      propertyType = propertyType.describe(propSchema.description);
    }
    shape[key] = propertyType;
  }
  return shape;
}
function toZodType(schema) {
  if (!schema) return z.any();
  let zType = zodForConst(schema) ?? zodForEnum(schema) ?? zodForUnion(schema) ?? zodForType(schema);
  if (isNullable(schema)) zType = zType.nullable();
  if (schema.description) zType = zType.describe(schema.description);
  return zType;
}
function zodForType(schema) {
  const types = getNonNullTypes(schema);
  if (types.length > 1) return z.any();
  if (Array.isArray(schema.type) && types.length === 0) return z.null();
  const type = types[0] ?? (schema.properties ? "object" : void 0);
  switch (type) {
    case "string":
      return z.string();
    case "number":
      return z.number();
    case "integer":
      return z.number().int();
    case "boolean":
      return z.boolean();
    case "array":
      return z.array(
        Array.isArray(schema.items) ? z.any() : toZodType(schema.items)
      );
    case "object":
      return toZodObject(schema);
    case "null":
      return z.null();
    default:
      return z.any();
  }
}
function zodForConst(schema) {
  if (!("const" in schema)) return void 0;
  return isJsonLiteral(schema.const) ? z.literal(schema.const) : z.any();
}
function zodForEnum(schema) {
  if (!Array.isArray(schema.enum)) return void 0;
  if (!schema.enum.every(isJsonLiteral)) return z.any();
  return zodForLiterals(schema.enum);
}
function zodForUnion(schema) {
  const unionSchemas = schema.anyOf ?? schema.oneOf;
  if (!unionSchemas || unionSchemas.length < 2) return void 0;
  const options = unionSchemas.map((item) => toZodType(item));
  return z.union(options);
}
function zodForLiterals(values) {
  if (values.length === 0) return z.any();
  if (values.length === 1) return z.literal(values[0]);
  const literals = values.map((value) => z.literal(value));
  return z.union(literals);
}
function getNonNullTypes(schema) {
  return Array.isArray(schema.type) ? schema.type.filter((type) => type !== "null") : [schema.type].filter(Boolean);
}
function isNullable(schema) {
  return schema.nullable === true || Array.isArray(schema.type) && schema.type.includes("null");
}
function isJsonSchemaObject(input) {
  return input != null && typeof input === "object" && !Array.isArray(input);
}
function isJsonLiteral(value) {
  return value === null || typeof value === "string" || typeof value === "boolean" || typeof value === "number" && Number.isFinite(value);
}

// src/bridge/tool-filtering.ts
var PUBLIC_TO_NATIVE = {
  read: "Read",
  write: "Write",
  edit: "Edit",
  bash: "Bash",
  glob: "Glob",
  grep: "Grep",
  webSearch: "WebSearch",
  WebFetch: "WebFetch",
  NotebookEdit: "NotebookEdit",
  TodoWrite: "TodoWrite",
  Agent: "Agent",
  TaskCreate: "TaskCreate",
  TaskGet: "TaskGet",
  TaskUpdate: "TaskUpdate",
  TaskList: "TaskList",
  TaskStop: "TaskStop",
  TaskOutput: "TaskOutput",
  Monitor: "Monitor",
  ListMcpResources: "ListMcpResources",
  ReadMcpResource: "ReadMcpResource",
  ExitPlanMode: "ExitPlanMode",
  EnterWorktree: "EnterWorktree",
  ExitWorktree: "ExitWorktree",
  askUserQuestions: "AskUserQuestion",
  Skill: "Skill",
  ToolSearch: "ToolSearch"
};
var PUBLIC_TOOL_NAMES = Object.keys(PUBLIC_TO_NATIVE);
function toNativeName(toolName) {
  return PUBLIC_TO_NATIVE[toolName] ?? toolName;
}
function resolveNativeTools(toolFiltering) {
  if (toolFiltering == null || toolFiltering.mode === "deny") return void 0;
  return toolFiltering.toolNames.map((name) => toNativeName(name));
}
function resolveInactiveNativeTools(toolFiltering) {
  if (toolFiltering == null) return [];
  const inactiveToolNames = toolFiltering.mode === "allow" ? PUBLIC_TOOL_NAMES.filter(
    (name) => !toolFiltering.toolNames.includes(name)
  ) : toolFiltering.toolNames;
  return inactiveToolNames.map((name) => toNativeName(name));
}

// src/bridge/question-tool.ts
function claudeCodeQuestionKey(nativeInput) {
  return JSON.stringify(nativeInput.questions);
}
function toHarnessQuestionsInput(nativeInput) {
  return {
    allowPartialAnswers: true,
    questions: nativeInput.questions.map((question, questionIndex) => ({
      id: `question-${questionIndex + 1}`,
      question: question.question,
      header: question.header,
      options: question.options.map((option, optionIndex) => ({
        id: `option-${optionIndex + 1}`,
        label: option.label,
        description: option.description,
        ...option.preview !== void 0 ? { preview: option.preview } : {}
      })),
      allowMultiple: question.multiSelect,
      allowFreeForm: true
    }))
  };
}
function toClaudeCodeQuestionResult(input) {
  const output = input.output;
  if (output.action === "declined" || output.action === "cancelled") {
    return {
      behavior: "deny",
      message: `The user ${output.action} the questions.`
    };
  }
  const canonicalInput = toHarnessQuestionsInput(input.nativeInput);
  const answers = Object.fromEntries(
    canonicalInput.questions.flatMap((question, questionIndex) => {
      const answer = output.answers[question.id];
      if (answer == null) return [];
      const nativeQuestion = input.nativeInput.questions[questionIndex];
      return [
        [
          nativeQuestion.question,
          toClaudeCodeAnswer({
            nativeQuestion,
            optionIds: answer.optionIds,
            freeform: answer.freeform
          })
        ]
      ];
    })
  );
  return {
    behavior: "allow",
    updatedInput: { ...input.nativeInput, answers }
  };
}
function toClaudeCodeAnswer(input) {
  if (input.freeform !== void 0) return input.freeform;
  const labels = input.optionIds.flatMap((optionId) => {
    const index = positionalIdIndex({ id: optionId, prefix: "option-" });
    const option = index == null ? void 0 : input.nativeQuestion.options[index];
    return option == null ? [] : [option.label];
  });
  return input.nativeQuestion.multiSelect ? labels.join(", ") : labels[0] ?? "";
}
function positionalIdIndex(input) {
  if (!input.id.startsWith(input.prefix)) return void 0;
  const oneBasedIndex = Number(input.id.slice(input.prefix.length));
  return Number.isInteger(oneBasedIndex) && oneBasedIndex > 0 ? oneBasedIndex - 1 : void 0;
}

// src/bridge/index.ts
var NATIVE_TO_COMMON = {
  Read: "read",
  Write: "write",
  Edit: "edit",
  Bash: "bash",
  Glob: "glob",
  Grep: "grep",
  WebSearch: "webSearch",
  AskUserQuestion: "askUserQuestions"
};
var NATIVE_TOOL_KINDS = {
  Read: "readonly",
  Glob: "readonly",
  Grep: "readonly",
  WebSearch: "readonly",
  WebFetch: "readonly",
  TaskGet: "readonly",
  TaskList: "readonly",
  TaskOutput: "readonly",
  ListMcpResources: "readonly",
  ReadMcpResource: "readonly",
  Write: "edit",
  Edit: "edit",
  NotebookEdit: "edit",
  TodoWrite: "edit",
  TaskCreate: "edit",
  TaskUpdate: "edit",
  TaskStop: "edit",
  EnterWorktree: "edit",
  ExitWorktree: "edit",
  ExitPlanMode: "edit",
  Skill: "readonly",
  AskUserQuestion: "readonly",
  ToolSearch: "readonly",
  Bash: "bash",
  Monitor: "bash"
};
function toCommonName(nativeName) {
  return NATIVE_TO_COMMON[nativeName] ?? nativeName;
}
var args = parseArgs(argv.slice(2));
var workdir = args.workdir;
var bridgeStateDir = args.bridgeStateDir;
if (!workdir) {
  emitFatal("Missing --workdir argument.");
}
if (!bridgeStateDir) {
  emitFatal("Missing --bridge-state-dir argument.");
}
var claudeSdk = claudeAgentSdk;
var mcpModule = mcpServerModule;
var lastClaudeSessionId;
await runBridge({
  bridgeType: "claude-code",
  bridgeStateDir,
  onStart: runTurn,
  // Claude Code's conversation state lives in the runtime's own store, keyed
  // by working directory. The resume payload names the exact conversation so a
  // later resume does not have to fall back to "most recent in this workdir".
  onStop: () => lastClaudeSessionId == null ? {} : { claudeSessionId: lastClaudeSessionId }
});
function createPermissionOptions(input) {
  const permissionMode = input.start.permissionMode ?? "allow-all";
  const inactiveNativeTools = new Set(input.inactiveNativeTools);
  const permissionSettings = createPermissionSettings({
    permissionMode,
    inactiveNativeTools
  });
  const baseOptions = {
    permissionMode: permissionMode === "allow-all" ? "bypassPermissions" : permissionMode === "allow-edits" ? "acceptEdits" : "default",
    allowDangerouslySkipPermissions: permissionMode === "allow-all",
    ...permissionSettings ? { settings: permissionSettings } : {}
  };
  if (permissionMode === "allow-all") {
    return {
      ...baseOptions,
      /*
       * Claude Code exposes AskUserQuestion in headless SDK sessions only
       * when a permission prompt tool is configured. The stdio prompt tool
       * preserves that tool surface without supplying the canUseTool callback
       * that bypassPermissions guarantees it will never invoke.
       */
      permissionPromptToolName: "stdio"
    };
  }
  return {
    ...baseOptions,
    canUseTool: async (toolName, toolInput, options) => {
      if (toolName.startsWith("mcp__harness-tools__")) {
        return { behavior: "allow", updatedInput: toolInput };
      }
      if (!inactiveNativeTools.has(toolName) && !nativeToolRequiresApproval({
        nativeName: toolName,
        permissionMode
      })) {
        return { behavior: "allow", updatedInput: toolInput };
      }
      const approvalId = options.toolUseID;
      input.approvalRequestedToolUseIds.add(approvalId);
      input.nativeToolCallNames.set(approvalId, toolName);
      input.emit({
        type: "tool-call",
        toolCallId: approvalId,
        toolName: toCommonName(toolName),
        nativeName: toolName,
        input: JSON.stringify(toolInput ?? {}),
        providerExecuted: true,
        ...isExternalMcpTool(toolName) ? { dynamic: true } : {}
      });
      input.emit({
        type: "tool-approval-request",
        approvalId,
        toolCallId: approvalId
      });
      input.finishApprovalStep(approvalId);
      const decision = await input.turn.requestToolApproval(approvalId);
      return decision.approved ? { behavior: "allow", updatedInput: toolInput, toolUseID: approvalId } : {
        behavior: "deny",
        message: decision.reason ?? "Denied",
        toolUseID: approvalId
      };
    }
  };
}
function createQuestionPreToolUseHook(input) {
  return async (hookInput, toolUseID) => {
    if (hookInput.hook_event_name !== "PreToolUse" || hookInput.tool_name !== "AskUserQuestion") {
      return {};
    }
    const nativeInput = hookInput.tool_input;
    const canonicalInput = toHarnessQuestionsInput(nativeInput);
    const toolCallId = toolUseID ?? hookInput.tool_use_id;
    input.nativeToolCallNames.set(toolCallId, hookInput.tool_name);
    input.emit({
      type: "tool-call",
      toolCallId,
      toolName: "askUserQuestions",
      nativeName: hookInput.tool_name,
      input: JSON.stringify(canonicalInput),
      providerExecuted: false,
      providerMetadata: {
        "claude-code": {
          nativeRequest: nativeInput
        }
      }
    });
    const questionKey = claudeCodeQuestionKey(nativeInput);
    const result = await input.turn.requestToolResult({
      toolCallId,
      matches: (candidate) => {
        const nativeRequest = candidate.toolResult?.providerOptions?.["claude-code"]?.nativeRequest;
        return nativeRequest != null && claudeCodeQuestionKey(nativeRequest) === questionKey;
      }
    });
    const nativeResult = toClaudeCodeQuestionResult({
      nativeInput,
      output: result.output
    });
    return {
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        ...nativeResult.behavior === "allow" ? {
          permissionDecision: "allow",
          updatedInput: {
            ...nativeResult.updatedInput
          }
        } : {
          permissionDecision: "deny",
          permissionDecisionReason: nativeResult.message
        }
      }
    };
  };
}
function createPermissionSettings(input) {
  const askRules = /* @__PURE__ */ new Set();
  for (const [nativeName, kind] of Object.entries(NATIVE_TOOL_KINDS)) {
    if (input.inactiveNativeTools.has(nativeName) || (input.permissionMode === "allow-reads" ? kind === "edit" || kind === "bash" : input.permissionMode === "allow-edits" ? kind === "bash" : false)) {
      askRules.add(`${nativeName}(*)`);
    }
  }
  if (askRules.size === 0) return void 0;
  return {
    permissions: { ask: [...askRules] },
    sandbox: { autoAllowBashIfSandboxed: false }
  };
}
function nativeToolRequiresApproval(input) {
  if (input.permissionMode === "allow-all") return false;
  const kind = NATIVE_TOOL_KINDS[input.nativeName] ?? "edit";
  if (input.permissionMode === "allow-edits") return kind === "bash";
  return kind === "edit" || kind === "bash";
}
async function runTurn(start, turn) {
  const emit = (msg) => turn.emit(msg);
  const abortCtl = new AbortController();
  let gracefulAbort;
  let hardAbortTimer;
  const onHostAbort = () => {
    if (gracefulAbort) {
      gracefulAbort();
    } else {
      abortCtl.abort();
    }
  };
  if (turn.abortSignal.aborted) {
    abortCtl.abort();
  } else {
    turn.abortSignal.addEventListener("abort", onHostAbort, { once: true });
  }
  const streamEventState = createClaudeStreamEventState();
  const mcpServers = { ...start.mcpServers };
  if (start.tools && start.tools.length > 0) {
    const server = new mcpModule.McpServer({
      name: "harness-tools",
      version: "1.0.0"
    });
    for (const tool of start.tools) {
      server.registerTool(
        tool.name,
        {
          description: tool.description ?? "",
          inputSchema: jsonSchemaToZodObject(tool.inputSchema)
        },
        async (...handlerArgs) => {
          const [input, extra] = handlerArgs;
          const metadataToolCallId = extra._meta?.["claudecode/toolUseId"];
          const toolCallId = typeof metadataToolCallId === "string" ? metadataToolCallId : randomUUID3();
          emit({
            type: "tool-call",
            toolCallId,
            toolName: tool.name,
            input: JSON.stringify(input),
            providerExecuted: false
          });
          const { output, isError } = await turn.requestToolResult(toolCallId);
          emit({
            type: "tool-result",
            toolCallId,
            toolName: tool.name,
            result: output ?? null,
            isError: !!isError
          });
          return {
            content: [{ type: "text", text: JSON.stringify(output ?? null) }],
            isError
          };
        }
      );
    }
    mcpServers["harness-tools"] = {
      type: "sdk",
      name: "harness-tools",
      instance: server
    };
  }
  const compaction = createCompactionLatch((event) => emit(event));
  const queryInput = createQueryInput({
    initialUserMessage: start.prompt,
    userMessages: turn.experimental_userMessages,
    abortSignal: abortCtl.signal
  });
  const skillsOption = toClaudeSkillsOption(start.skills);
  const nativeTools = resolveNativeTools(start.builtinToolFiltering);
  const inactiveNativeTools = resolveInactiveNativeTools(
    start.builtinToolFiltering
  );
  const permissionOptions = createPermissionOptions({
    start,
    inactiveNativeTools,
    turn,
    emit,
    finishApprovalStep: (approvalId) => {
      finishApprovalStep({ state: streamEventState, emit, approvalId });
    },
    nativeToolCallNames: streamEventState.nativeToolCallNames,
    approvalRequestedToolUseIds: streamEventState.approvalRequestedToolUseIds
  });
  const questionPreToolUseHook = createQuestionPreToolUseHook({
    turn,
    emit,
    nativeToolCallNames: streamEventState.nativeToolCallNames
  });
  const q = claudeSdk.query({
    prompt: queryInput.input,
    options: {
      ...start.model ? { model: start.model } : {},
      ...start.maxTurns !== void 0 ? { maxTurns: start.maxTurns } : {},
      ...start.env !== void 0 ? { env: { ...procEnv2, ...start.env } } : {},
      ...skillsOption ? { skills: skillsOption } : {},
      ...nativeTools !== void 0 ? { tools: nativeTools } : {},
      ...inactiveNativeTools.length > 0 ? { disallowedTools: inactiveNativeTools } : {},
      systemPrompt: createClaudeCodeSystemPrompt(start.instructions),
      thinking: start.thinking,
      ...start.effort !== void 0 ? { effort: start.effort } : {},
      ...start.responseFormat?.type === "json" && start.responseFormat.schema != null ? {
        outputFormat: {
          type: "json_schema",
          schema: start.responseFormat.schema
        }
      } : {},
      includePartialMessages: true,
      // The `PostCompact` hook carries the compaction summary, which the
      // `compact_boundary` system message does not. Latch it for the unified
      // `compaction` event; return an empty output so compaction proceeds.
      hooks: {
        PreToolUse: [
          {
            matcher: "AskUserQuestion",
            hooks: [questionPreToolUseHook]
          }
        ],
        PostCompact: [
          {
            hooks: [
              async (input) => {
                if (typeof input?.compact_summary === "string") {
                  compaction.onSummary(input.compact_summary);
                }
                return {};
              }
            ]
          }
        ]
      },
      // Continuation rule, most specific first.
      //
      // `resumeSessionId` names the exact conversation and is what a
      // cross-process resume should use: `continue` means "most recent thread
      // in this workdir", which silently picks the wrong one once anything
      // else has run there. The bridge also retains the id observed during its
      // previous query, so every later query stays pinned to that conversation
      // even when the host detached and reattached between turns. `resume` and
      // `continue` are mutually exclusive in the SDK.
      //
      // Otherwise the host can force-continue by setting `start.continue`,
      // and turns after the first fall back to the legacy cwd-based behavior
      // when no exact id was observed.
      ...start.resumeSessionId ?? lastClaudeSessionId ? { resume: start.resumeSessionId ?? lastClaudeSessionId } : start.continue === true || !turn.firstTurn ? { continue: true } : {},
      ...permissionOptions,
      mcpServers,
      cwd: workdir,
      abortSignal: abortCtl.signal
    }
  });
  gracefulAbort = () => {
    hardAbortTimer = setTimeout(() => abortCtl.abort(), 5e3);
    hardAbortTimer.unref?.();
    void Promise.resolve().then(() => q.interrupt()).catch(() => abortCtl.abort());
  };
  let turnUsage;
  let totalCostUsd;
  let emittedTerminalError = false;
  let emittedTerminalFinish = false;
  const emitTerminalError = (message) => {
    const normalized = message?.trim();
    if (!normalized || emittedTerminalError || emittedTerminalFinish) return;
    streamEventState.observedTerminalError = normalized;
    emittedTerminalError = true;
    if (!turn.abortSignal.aborted) {
      turn.emitError({
        error: normalized,
        message: "claude-code terminal error"
      });
    }
    queryInput.close();
    abortCtl.abort();
  };
  const emitStreamEvent = createEmitStreamEvent({
    state: streamEventState,
    emit,
    emitWarning: turn.emitWarning,
    emitTerminalError,
    onCompactionBoundary: (boundary) => compaction.onBoundary(boundary),
    toCommonName
  });
  try {
    for await (const msg of q) {
      if (abortCtl.signal.aborted) break;
      const type = msg.type;
      if (type === "command_lifecycle") {
        queryInput.handleLifecycle(msg);
      }
      const sessionId = msg.session_id;
      if (typeof sessionId === "string" && sessionId.length > 0) {
        lastClaudeSessionId = sessionId;
      }
      emitStreamEvent(msg);
      if (type === "result") {
        if (msg.subtype === "success") {
          if (msg.is_error) {
            emitTerminalError(
              msg.result?.trim() || streamEventState.observedTerminalError || (typeof msg.api_error_status === "number" ? `Claude Code reported an API error (HTTP ${msg.api_error_status})` : "Claude Code reported a failed result")
            );
            continue;
          }
          const emptyResult = !msg.result?.trim?.();
          if (emptyResult && streamEventState.observedTerminalError) {
            emitTerminalError(streamEventState.observedTerminalError);
            continue;
          }
          const usage = msg.usage ?? msg.message?.usage;
          const harnessUsage = mapUsage(usage);
          if (harnessUsage) turnUsage = addUsage(turnUsage, harnessUsage);
          if (typeof msg.total_cost_usd === "number") {
            totalCostUsd = (totalCostUsd ?? 0) + msg.total_cost_usd;
          }
          if (start.responseFormat?.type === "json" && msg.structured_output !== void 0) {
            const id = randomUUID3();
            emit({ type: "text-start", id });
            emit({
              type: "text-delta",
              id,
              delta: JSON.stringify(msg.structured_output)
            });
            emit({ type: "text-end", id });
            streamEventState.stepOpen = true;
          }
          if (streamEventState.stepOpen) {
            emitFinishStep({
              state: streamEventState,
              emit,
              usage: streamEventState.pendingStepUsage ?? harnessUsage
            });
          }
          queryInput.observeResult();
          if (!queryInput.hasActiveUserMessages()) {
            queryInput.close();
            break;
          }
        } else {
          emitTerminalError(
            (Array.isArray(msg.errors) ? msg.errors.join("\n") : void 0) || streamEventState.observedTerminalError || msg.result || "Unknown error"
          );
        }
        continue;
      }
      if (queryInput.hasObservedResult && !queryInput.hasActiveUserMessages()) {
        queryInput.close();
        break;
      }
    }
  } catch (err) {
    if (!turn.abortSignal.aborted && !(abortCtl.signal.aborted && emittedTerminalError)) {
      turn.emitError({ error: err, message: "claude-code turn failed" });
    }
    return;
  } finally {
    gracefulAbort = void 0;
    if (hardAbortTimer != null) clearTimeout(hardAbortTimer);
    turn.abortSignal.removeEventListener("abort", onHostAbort);
    queryInput.close();
    try {
      await q.return?.(
        void 0
      );
    } catch {
    }
  }
  if (emittedTerminalError) return;
  emittedTerminalFinish = true;
  void emittedTerminalFinish;
  emit({
    type: "finish",
    finishReason: { unified: "stop", raw: "stop" },
    totalUsage: turnUsage ?? streamEventState.stepUsage ?? defaultUsage(),
    ...totalCostUsd !== void 0 || lastClaudeSessionId !== void 0 ? {
      harnessMetadata: {
        "claude-code": {
          ...totalCostUsd !== void 0 ? { costUsd: totalCostUsd } : {},
          // The conversation this turn belongs to, resumable outside the
          // SDK with `claude --resume <sessionId>` and captured by the
          // adapter for exact cross-process resume.
          ...lastClaudeSessionId !== void 0 ? { sessionId: lastClaudeSessionId } : {}
        }
      }
    } : {}
  });
}
function createQueryInput({
  initialUserMessage,
  userMessages,
  abortSignal
}) {
  let closed = false;
  let observedResult = false;
  const submittedMessages = /* @__PURE__ */ new Map();
  const close = (error) => {
    if (closed) return;
    closed = true;
    userMessages.close(error);
  };
  if (abortSignal.aborted) {
    close(abortSignal.reason);
  } else {
    abortSignal.addEventListener("abort", () => close(abortSignal.reason), {
      once: true
    });
  }
  const toUserMessage = (options) => ({
    type: "user",
    message: {
      role: "user",
      content: [{ type: "text", text: options.text }]
    },
    parent_tool_use_id: null,
    uuid: options.messageId,
    ...options.priority == null ? {} : { priority: options.priority }
  });
  const messageIterator = userMessages[Symbol.asyncIterator]();
  return {
    close,
    handleLifecycle: (message) => {
      const lifecycle = message;
      if (lifecycle.command_uuid == null || lifecycle.state == null) return;
      const submitted = submittedMessages.get(lifecycle.command_uuid);
      if (submitted == null) return;
      if (lifecycle.state === "queued" || lifecycle.state === "started") {
        submitted.accept();
        return;
      }
      if (lifecycle.state === "cancelled" || lifecycle.state === "discarded") {
        submitted.reject(
          new Error(`Claude Code ${lifecycle.state} the user message.`)
        );
      }
      submittedMessages.delete(lifecycle.command_uuid);
    },
    hasActiveUserMessages: () => submittedMessages.size > 0 || userMessages.pendingCount > 0,
    observeResult: () => {
      observedResult = true;
    },
    get hasObservedResult() {
      return observedResult;
    },
    input: {
      [Symbol.asyncIterator]() {
        let sentInitial = false;
        return {
          async next() {
            if (closed || abortSignal.aborted) {
              return {
                value: void 0,
                done: true
              };
            }
            if (!sentInitial) {
              sentInitial = true;
              return {
                value: toUserMessage({
                  text: initialUserMessage,
                  messageId: randomUUID3()
                }),
                done: false
              };
            }
            const nextMessage = await messageIterator.next();
            if (nextMessage.done) {
              return {
                value: void 0,
                done: true
              };
            }
            submittedMessages.set(
              nextMessage.value.messageId,
              nextMessage.value
            );
            return {
              value: toUserMessage({
                text: nextMessage.value.text,
                messageId: nextMessage.value.messageId,
                priority: "next"
              }),
              done: false
            };
          }
        };
      }
    }
  };
}
function addUsage(total, usage) {
  if (total == null) return usage;
  const result = { ...total };
  for (const [key, value] of Object.entries(usage)) {
    const previous = result[key];
    if (typeof value === "number" && typeof previous === "number") {
      result[key] = previous + value;
    } else if (value != null && previous != null && typeof value === "object" && typeof previous === "object" && !Array.isArray(value) && !Array.isArray(previous)) {
      result[key] = addUsage(
        previous,
        value
      );
    } else {
      result[key] = value;
    }
  }
  return result;
}
function parseArgs(args2) {
  const out = {};
  for (let i = 0; i < args2.length; i++) {
    if (args2[i] === "--workdir" && i + 1 < args2.length) {
      out.workdir = args2[++i];
    } else if (args2[i] === "--bridge-state-dir" && i + 1 < args2.length) {
      out.bridgeStateDir = args2[++i];
    }
  }
  return out;
}
function emitFatal(message) {
  stdout2.write(JSON.stringify({ type: "bridge-fatal", message }) + "\n");
  process.exit(1);
}
//# sourceMappingURL=index.mjs.map