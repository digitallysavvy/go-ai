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

// src/bridge/index.ts
import { mkdir as mkdir2, writeFile as writeFile2 } from "fs/promises";

// src/bridge/cli-relay.ts
var CLI_SHIM_FILENAME = "harness-tool.mjs";
function buildCliShimScript({
  relayPort
}) {
  return `#!/usr/bin/env node
const [toolName, inputJson = '{}'] = process.argv.slice(2);
if (!toolName) {
  console.error('Usage: harness-tool <tool_name> <json_input>');
  process.exit(64);
}
let input;
try {
  input = JSON.parse(inputJson);
} catch (error) {
  console.error('Invalid JSON input: ' + (error instanceof Error ? error.message : String(error)));
  process.exit(64);
}
const requestId = 'cli-' + Date.now() + '-' + Math.random().toString(16).slice(2);
const response = await fetch('http://127.0.0.1:${relayPort}', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ requestId, toolName, input }),
});
const text = await response.text();
let payload;
try {
  payload = text ? JSON.parse(text) : {};
} catch {
  payload = { error: text };
}
if (!response.ok || payload.error) {
  console.error(String(payload.error ?? ('tool relay failed with HTTP ' + response.status)));
  process.exit(1);
}
console.log(JSON.stringify(payload.result ?? payload, null, 2));
`;
}
function parseToolRelayCommands({
  command,
  cliShimPath
}) {
  return parseToolRelayCommandsInternal({ command, cliShimPath, depth: 0 });
}
function parseToolRelayCommandsInternal({
  command,
  cliShimPath,
  depth
}) {
  const commands = splitShellAndCommands(command);
  if (!commands) return void 0;
  if (commands.length > 1) {
    const relayCalls = [];
    for (const nestedCommand of commands) {
      const nestedCalls = parseToolRelayCommandsInternal({
        command: nestedCommand,
        cliShimPath,
        depth
      });
      if (!nestedCalls) return void 0;
      relayCalls.push(...nestedCalls);
    }
    return relayCalls;
  }
  const argv2 = parseShellWords(command);
  if (!argv2) return void 0;
  const relayCall = parseDirectToolRelayArgv({ argv: argv2, cliShimPath });
  if (relayCall) return [relayCall];
  const innerCommand = extractShellEvalCommand(argv2);
  if (!innerCommand || depth >= 2) return void 0;
  return parseToolRelayCommandsInternal({
    command: innerCommand,
    cliShimPath,
    depth: depth + 1
  });
}
function parseDirectToolRelayArgv({
  argv: argv2,
  cliShimPath
}) {
  if (argv2.length < 3 || argv2.length > 4) return void 0;
  if (argv2[0] !== "node" || argv2[1] !== cliShimPath) return void 0;
  const toolName = argv2[2];
  if (!toolName) return void 0;
  try {
    return { toolName, input: JSON.parse(argv2[3] ?? "{}") };
  } catch {
    return void 0;
  }
}
function extractShellEvalCommand(argv2) {
  if (argv2.length !== 3) return void 0;
  const shellName = argv2[0].split("/").at(-1);
  if (shellName !== "bash" && shellName !== "sh" && shellName !== "zsh") {
    return void 0;
  }
  if (argv2[1] !== "-c" && argv2[1] !== "-lc") return void 0;
  return argv2[2];
}
function splitShellAndCommands(command) {
  const commands = [];
  let start = 0;
  let quote;
  for (let i = 0; i < command.length; i++) {
    const char = command[i];
    if (quote === "'") {
      if (char === "'") quote = void 0;
      continue;
    }
    if (quote === '"') {
      if (char === '"') {
        quote = void 0;
      } else if (char === "\\") {
        i++;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === "\\") {
      i++;
      continue;
    }
    if (char === "&" && command[i + 1] === "&") {
      const nestedCommand2 = command.slice(start, i).trim();
      if (!nestedCommand2) return void 0;
      commands.push(nestedCommand2);
      start = i + 2;
      i++;
    }
  }
  if (quote) return void 0;
  const nestedCommand = command.slice(start).trim();
  if (!nestedCommand) return void 0;
  commands.push(nestedCommand);
  return commands;
}
function parseShellWords(command) {
  const words = [];
  let current = "";
  let quote;
  let hasCurrent = false;
  const pushCurrent = () => {
    if (!hasCurrent) return;
    words.push(current);
    current = "";
    hasCurrent = false;
  };
  for (let i = 0; i < command.length; i++) {
    const char = command[i];
    if (quote === "'") {
      if (char === "'") {
        quote = void 0;
      } else {
        current += char;
      }
      hasCurrent = true;
      continue;
    }
    if (quote === '"') {
      if (char === '"') {
        quote = void 0;
      } else if (char === "\\" && i + 1 < command.length) {
        current += command[++i];
      } else {
        current += char;
      }
      hasCurrent = true;
      continue;
    }
    if (/\s/.test(char)) {
      pushCurrent();
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      hasCurrent = true;
      continue;
    }
    if (char === "\\" && i + 1 < command.length) {
      current += command[++i];
      hasCurrent = true;
      continue;
    }
    if (/[;&|<>()`$]/.test(char)) return void 0;
    current += char;
    hasCurrent = true;
  }
  if (quote) return void 0;
  pushCurrent();
  return words;
}

// src/bridge/codex-step-tracker.ts
function createCodexStepTracker(input) {
  let stepOpen = false;
  const pendingToolItemIds = /* @__PURE__ */ new Set();
  const finishStep = () => {
    if (!stepOpen || pendingToolItemIds.size > 0) return;
    input.send({
      type: "finish-step",
      finishReason: { unified: "stop", raw: "stop" },
      usage: defaultUsage(),
      harnessMetadata: { codex: { inferredStep: true } }
    });
    stepOpen = false;
  };
  return {
    observeEvent({ event, itemId }) {
      const item = event.item;
      if (!item || !isStepItem(item)) return;
      stepOpen = true;
      if (isToolStepItem(item)) {
        if (event.type === "item.started" && itemId) {
          pendingToolItemIds.add(itemId);
        } else if (event.type === "item.completed") {
          if (itemId) pendingToolItemIds.delete(itemId);
          finishStep();
        }
      }
    },
    finishTurn() {
      pendingToolItemIds.clear();
      finishStep();
    }
  };
}
function isStepItem(item) {
  return isModelStepItem(item) || isToolStepItem(item);
}
function isModelStepItem(item) {
  return item.type === "reasoning" || item.type === "agent_message";
}
function isToolStepItem(item) {
  return item.type === "command_execution" || item.type === "mcp_tool_call" || item.type === "web_search" || item.type === "file_change" || item.type === "todo_list";
}
function defaultUsage() {
  return {
    inputTokens: { total: 0, noCache: 0, cacheRead: 0, cacheWrite: 0 },
    outputTokens: { total: 0, text: 0 }
  };
}

// src/bridge/create-emit-stream-event.ts
import { randomUUID as randomUUID2 } from "crypto";
var NATIVE_TO_COMMON = {
  shell: "bash",
  web_search: "webSearch"
};
function toCommonName(nativeName) {
  return NATIVE_TO_COMMON[nativeName] ?? nativeName;
}
function createEmitStreamEvent({
  send,
  stepTracker,
  setTurnUsage,
  setThreadId,
  emitWarning,
  emitError
}) {
  const textByItem = /* @__PURE__ */ new Map();
  const reasoningByItem = /* @__PURE__ */ new Map();
  const emittedWebSearchToolCalls = /* @__PURE__ */ new Set();
  return (event) => {
    if (event.type === "thread.started" && typeof event.thread_id === "string") {
      setThreadId(event.thread_id);
      send({ type: "bridge-thread", threadId: event.thread_id });
    }
    if (event.type === "turn.completed") {
      if (event.usage) setTurnUsage(mapUsage(event.usage));
      stepTracker.finishTurn();
      return;
    }
    if (event.type === "turn.failed") {
      emitError({
        error: event.error?.message ?? "codex turn failed",
        message: "codex turn failed"
      });
      return;
    }
    if (event.type === "error") {
      emitError({
        error: event.message ?? "codex error",
        message: "codex stream error"
      });
      return;
    }
    if (!event.item) return;
    const item = event.item;
    const id = item.id ?? randomUUID2();
    const observeStep = () => {
      stepTracker.observeEvent({ event, itemId: id });
    };
    if (item.type === "agent_message" && typeof item.text === "string") {
      if (!textByItem.has(id)) {
        send({ type: "text-start", id });
        textByItem.set(id, "");
      }
      const last = textByItem.get(id) ?? "";
      const next = item.text;
      if (next.length > last.length) {
        send({ type: "text-delta", id, delta: next.slice(last.length) });
        textByItem.set(id, next);
      }
      if (event.type === "item.completed") send({ type: "text-end", id });
      observeStep();
      return;
    }
    if (item.type === "reasoning" && typeof item.text === "string") {
      if (!reasoningByItem.has(id)) {
        send({ type: "reasoning-start", id });
        reasoningByItem.set(id, "");
      }
      const last = reasoningByItem.get(id) ?? "";
      const next = item.text;
      if (next.length > last.length) {
        send({ type: "reasoning-delta", id, delta: next.slice(last.length) });
        reasoningByItem.set(id, next);
      }
      if (event.type === "item.completed") send({ type: "reasoning-end", id });
      observeStep();
      return;
    }
    if (item.type === "command_execution") {
      const nativeName = "shell";
      if (event.type === "item.started") {
        send({
          type: "tool-call",
          toolCallId: id,
          toolName: toCommonName(nativeName),
          nativeName,
          input: JSON.stringify({ command: item.command ?? "" }),
          providerExecuted: true
        });
      } else if (event.type === "item.completed") {
        send({
          type: "tool-result",
          toolCallId: id,
          toolName: toCommonName(nativeName),
          result: {
            exitCode: item.exit_code ?? null,
            output: item.aggregated_output ?? "",
            status: item.status ?? "completed"
          }
        });
      }
      observeStep();
      return;
    }
    if (item.type === "mcp_tool_call") {
      if (event.type === "item.started") {
        send({
          type: "tool-call",
          toolCallId: id,
          toolName: item.tool ?? "unknown",
          nativeName: item.tool ?? "unknown",
          input: JSON.stringify(item.arguments ?? {}),
          providerExecuted: true,
          dynamic: true
        });
      } else if (event.type === "item.completed") {
        send({
          type: "tool-result",
          toolCallId: id,
          toolName: item.tool ?? "unknown",
          result: extractMcpToolCallResult(item),
          dynamic: true
        });
      }
      observeStep();
      return;
    }
    if (item.type === "web_search") {
      const nativeName = "web_search";
      const query = getWebSearchQuery(item);
      if (event.type === "item.started" || event.type === "item.updated") {
        if (query !== void 0) {
          emitWebSearchToolCall({
            id,
            query,
            nativeName,
            emittedWebSearchToolCalls,
            send
          });
        }
      } else if (event.type === "item.completed") {
        emitWebSearchToolCall({
          id,
          query: query ?? "",
          nativeName,
          emittedWebSearchToolCalls,
          send
        });
        send({
          type: "tool-result",
          toolCallId: id,
          toolName: toCommonName(nativeName),
          result: item.result ?? item.action ?? null
        });
        emittedWebSearchToolCalls.delete(id);
      }
      observeStep();
      return;
    }
    if (item.type === "file_change" && event.type === "item.completed") {
      for (const change of item.changes ?? []) {
        send({
          type: "file-change",
          event: change.kind === "add" ? "create" : change.kind === "delete" ? "delete" : "modify",
          path: change.path
        });
      }
      observeStep();
      return;
    }
    if (item.type === "error" && event.type === "item.completed") {
      const message = typeof item.message === "string" && item.message.trim() ? item.message : "codex reported a non-fatal error item";
      emitWarning({ message });
    }
  };
}
function getWebSearchQuery(item) {
  if (typeof item.query === "string" && item.query.length > 0) {
    return item.query;
  }
  if (typeof item.action?.query === "string" && item.action.query.length > 0) {
    return item.action.query;
  }
}
function emitWebSearchToolCall({
  id,
  query,
  nativeName,
  emittedWebSearchToolCalls,
  send
}) {
  if (emittedWebSearchToolCalls.has(id)) return;
  emittedWebSearchToolCalls.add(id);
  send({
    type: "tool-call",
    toolCallId: id,
    toolName: toCommonName(nativeName),
    nativeName,
    input: JSON.stringify({ query }),
    providerExecuted: true
  });
}
function extractMcpToolCallResult(item) {
  if (item.result === void 0 || item.result === null || typeof item.result !== "object") {
    return item.error?.message ? { error: item.error.message } : null;
  }
  const result = item.result;
  if (result.structured_content !== void 0 && result.structured_content !== null) {
    return result.structured_content;
  }
  return result.content ?? null;
}
function mapUsage(usage) {
  const input = usage.input_tokens ?? 0;
  const cacheRead = usage.cached_input_tokens ?? 0;
  return {
    inputTokens: {
      total: input,
      noCache: Math.max(0, input - cacheRead),
      cacheRead,
      cacheWrite: 0
    },
    outputTokens: {
      total: usage.output_tokens ?? 0,
      text: usage.output_tokens ?? 0
    }
  };
}

// src/bridge/tool-relay.ts
import { createServer } from "http";

// src/bridge/tool-relay-auth.ts
var ToolRelayAuthorizer = class {
  constructor({
    ttlMs = 1e4,
    now = Date.now
  } = {}) {
    this.authorizations = [];
    this.pendingRequests = [];
    this.ttlMs = ttlMs;
    this.now = now;
  }
  authorizeToolCall(call) {
    this.pruneExpired();
    const key = toolRelayCallKey(call);
    const pendingRequestIndex = this.pendingRequests.findIndex(
      (request) => request.key === key
    );
    if (pendingRequestIndex !== -1) {
      const [pendingRequest] = this.pendingRequests.splice(
        pendingRequestIndex,
        1
      );
      clearTimeout(pendingRequest.timeout);
      pendingRequest.resolve(true);
      return;
    }
    this.authorizations.push({
      key,
      expiresAt: this.now() + this.ttlMs
    });
  }
  waitForToolCallAuthorization(call) {
    this.pruneExpired();
    const key = toolRelayCallKey(call);
    const authorizationIndex = this.authorizations.findIndex(
      (authorization) => authorization.key === key
    );
    if (authorizationIndex !== -1) {
      this.authorizations.splice(authorizationIndex, 1);
      return Promise.resolve(true);
    }
    const expiresAt = this.now() + this.ttlMs;
    return new Promise((resolve) => {
      const pendingRequest = {
        key,
        expiresAt,
        timeout: setTimeout(() => {
          const index = this.pendingRequests.indexOf(pendingRequest);
          if (index !== -1) this.pendingRequests.splice(index, 1);
          resolve(false);
        }, this.ttlMs),
        resolve
      };
      this.pendingRequests.push(pendingRequest);
    });
  }
  close() {
    this.authorizations.length = 0;
    for (const pendingRequest of this.pendingRequests.splice(0)) {
      clearTimeout(pendingRequest.timeout);
      pendingRequest.resolve(false);
    }
  }
  pruneExpired() {
    const now = this.now();
    for (let i = this.authorizations.length - 1; i >= 0; i--) {
      if (this.authorizations[i].expiresAt <= now) {
        this.authorizations.splice(i, 1);
      }
    }
    for (let i = this.pendingRequests.length - 1; i >= 0; i--) {
      const pendingRequest = this.pendingRequests[i];
      if (pendingRequest.expiresAt <= now) {
        this.pendingRequests.splice(i, 1);
        clearTimeout(pendingRequest.timeout);
        pendingRequest.resolve(false);
      }
    }
  }
};
var ToolRelayPendingCalls = class {
  constructor() {
    this.calls = /* @__PURE__ */ new Map();
  }
  begin({
    call,
    run
  }) {
    const key = toolRelayCallKey(call);
    const existing = this.calls.get(key);
    if (existing) return { result: existing, isNew: false };
    const result = run();
    this.calls.set(key, result);
    void result.finally(() => {
      if (this.calls.get(key) === result) {
        this.calls.delete(key);
      }
    }).catch(() => {
    });
    return { result, isNew: true };
  }
};
function toolRelayCallKey({ toolName, input }) {
  return `${toolName}\0${canonicalJson(input ?? {})}`;
}
function canonicalJson(value) {
  return JSON.stringify(normalizeJsonValue(value));
}
function normalizeJsonValue(value) {
  if (Array.isArray(value)) {
    return value.map(normalizeJsonValue);
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).filter(([, entryValue]) => entryValue !== void 0).sort(([left], [right]) => left.localeCompare(right)).map(([key, entryValue]) => [key, normalizeJsonValue(entryValue)])
    );
  }
  return value;
}

// src/bridge/tool-relay.ts
async function startAuthorizedToolRelay({
  tools,
  emit,
  requestToolResult,
  authorizer = new ToolRelayAuthorizer()
}) {
  const toolNames = new Set(tools.map((tool) => tool.name));
  const pendingCalls = new ToolRelayPendingCalls();
  const server = createServer(async (req, res) => {
    try {
      if (req.method !== "POST" || req.url !== "/") {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "unauthorized tool relay request" }));
        return;
      }
      const chunks = [];
      for await (const chunk of req) {
        chunks.push(chunk);
      }
      const body = Buffer.concat(chunks).toString("utf8");
      const { requestId, toolName, input } = JSON.parse(body);
      if (!toolNames.has(toolName)) {
        res.writeHead(403, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({ error: `Tool "${toolName}" is not available` })
        );
        return;
      }
      const relayCall = { toolName, input };
      if (!await authorizer.waitForToolCallAuthorization(relayCall)) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "unauthorized tool relay request" }));
        return;
      }
      const { result } = pendingCalls.begin({
        call: relayCall,
        run: async () => {
          emit({
            type: "tool-call",
            toolCallId: requestId,
            toolName,
            input: JSON.stringify(input ?? {}),
            providerExecuted: false
          });
          const toolResult = await requestToolResult(requestId);
          emit({
            type: "tool-result",
            toolCallId: requestId,
            toolName,
            result: toolResult.output ?? null,
            isError: !!toolResult.isError
          });
          return toolResult;
        }
      });
      const { output } = await result;
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ result: output }));
    } catch (error) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          error: error instanceof Error ? error.message : String(error)
        })
      );
    }
  });
  await new Promise(
    (resolve) => server.listen(0, "127.0.0.1", () => resolve())
  );
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("tool relay did not expose a numeric port");
  }
  return {
    port: address.port,
    close: () => {
      authorizer.close();
      closeServer(server);
    },
    authorizeToolCall: (call) => authorizer.authorizeToolCall(call)
  };
}
function closeServer(server) {
  try {
    server.close();
  } catch {
  }
}

// src/bridge/index.ts
import { argv, env as procEnv2, stdout as stdout2 } from "process";
import * as codexSdkModule from "@openai/codex-sdk";
var args = parseArgs(argv.slice(2));
var workdir = requireArg({ value: args.workdir, name: "--workdir" });
var bridgeStateDir = requireArg({
  value: args.bridgeStateDir,
  name: "--bridge-state-dir"
});
var cliShimDir = requireArg({
  value: args.cliShimDir,
  name: "--cli-shim-dir"
});
var HARNESS_CLIENT_APP = procEnv2.AI_SDK_HARNESS_CLIENT_APP;
var codexSdk = codexSdkModule;
var threadState = { id: void 0 };
await runBridge({
  bridgeType: "codex",
  bridgeStateDir,
  onStart: runTurn,
  onStop: () => threadState.id ? { threadId: threadState.id } : {}
});
async function runTurn(start, turn) {
  const emit = (msg) => turn.emit(msg);
  if (start.restartThread) {
    threadState.id = void 0;
  } else if (typeof start.resumeThreadId === "string" && start.resumeThreadId.length > 0) {
    threadState.id = start.resumeThreadId;
  }
  let relay;
  let cliShimPath;
  if (start.tools && start.tools.length > 0) {
    cliShimPath = `${cliShimDir}/${CLI_SHIM_FILENAME}`;
    relay = await startToolRelay({
      tools: start.tools,
      emit,
      requestToolResult: turn.requestToolResult
    });
    await mkdir2(cliShimDir, { recursive: true });
    await writeFile2(
      cliShimPath,
      buildCliShimScript({ relayPort: relay.port }),
      "utf8"
    );
  }
  const codexConfig = {
    ...start.codexConfig,
    developer_instructions: [
      start.instructions,
      "Only respond with your `final` message once you have fully addressed the user request."
    ].filter((instruction) => Boolean(instruction)).join("\n\n"),
    model_reasoning_summary: "detailed"
  };
  const gatewayBaseUrl = procEnv2.AI_GATEWAY_BASE_URL;
  const hasGatewayAuth = Boolean(procEnv2.AI_GATEWAY_API_KEY || gatewayBaseUrl);
  if (hasGatewayAuth && !gatewayBaseUrl) {
    throw new Error(
      "AI Gateway auth was selected but AI_GATEWAY_BASE_URL is missing from the Codex bridge environment."
    );
  }
  const apiBaseUrl = hasGatewayAuth ? gatewayBaseUrl : procEnv2.OPENAI_BASE_URL ?? (start.headers != null ? "https://api.openai.com/v1" : void 0);
  const codexModel = start.model && hasGatewayAuth && !start.model.includes("/") ? `openai/${start.model}` : start.model;
  if (hasGatewayAuth && codexModel?.startsWith("openai/")) {
    codexConfig.model_supports_reasoning_summaries = true;
  }
  if (apiBaseUrl) {
    codexConfig.preferred_auth_method = "apikey";
    codexConfig.model_provider = "agent_bridge_openai";
    codexConfig.model_providers = {
      agent_bridge_openai: {
        name: procEnv2.CODEX_MODEL_PROVIDER_NAME || "Agent Bridge OpenAI",
        base_url: apiBaseUrl,
        env_key: "CODEX_API_KEY",
        wire_api: "responses",
        supports_websockets: false,
        ...start.headers != null || hasGatewayAuth && HARNESS_CLIENT_APP ? {
          http_headers: {
            ...start.headers,
            ...hasGatewayAuth && HARNESS_CLIENT_APP ? {
              "User-Agent": HARNESS_CLIENT_APP,
              "x-client-app": HARNESS_CLIENT_APP
            } : {}
          }
        } : {}
      }
    };
  }
  if (start.mcpServers != null) {
    codexConfig.mcp_servers = start.mcpServers;
  }
  const usesConfiguredModelProvider = typeof codexConfig.model_provider === "string";
  const codex = new codexSdk.Codex({
    ...procEnv2.CODEX_API_KEY ? { apiKey: procEnv2.CODEX_API_KEY } : {},
    ...!usesConfiguredModelProvider && apiBaseUrl ? { baseUrl: apiBaseUrl } : {},
    env: Object.fromEntries(
      Object.entries(procEnv2).filter(
        (entry) => typeof entry[1] === "string"
      )
    ),
    ...Object.keys(codexConfig).length > 0 ? { config: codexConfig } : {}
  });
  const threadOptions = {
    ...codexModel ? { model: codexModel } : {},
    sandboxMode: "danger-full-access",
    approvalPolicy: "never",
    workingDirectory: workdir,
    skipGitRepoCheck: true,
    ...start.reasoningEffort ? { modelReasoningEffort: start.reasoningEffort } : {},
    webSearchMode: start.webSearch ? "live" : "disabled"
  };
  const thread = threadState.id ? codex.resumeThread(threadState.id, threadOptions) : codex.startThread(threadOptions);
  emit({ type: "stream-start" });
  const userMessage = start.prompt;
  let turnUsage;
  const stepTracker = createCodexStepTracker({ send: emit });
  const emitStreamEvent = createEmitStreamEvent({
    send: emit,
    stepTracker,
    setTurnUsage: (usage) => turnUsage = usage,
    setThreadId: (threadId) => threadState.id = threadId,
    emitWarning: turn.emitWarning,
    emitError: turn.emitError
  });
  try {
    const { events } = await thread.runStreamed(userMessage, {
      signal: turn.abortSignal,
      ...start.responseFormat?.type === "json" && start.responseFormat.schema != null ? { outputSchema: start.responseFormat.schema } : {}
    });
    for await (const event of events) {
      if (turn.abortSignal.aborted) break;
      if (cliShimPath && event.item?.type === "command_execution") {
        const relayCalls = typeof event.item.command === "string" ? parseToolRelayCommands({
          command: event.item.command,
          cliShimPath
        }) : void 0;
        if (event.type === "item.started" && relay && relayCalls) {
          for (const relayCall of relayCalls) {
            relay.authorizeToolCall(relayCall);
          }
        }
        if (relayCalls) {
          stepTracker.observeEvent({ event, itemId: event.item.id });
          continue;
        }
      }
      emitStreamEvent(event);
    }
  } catch (err) {
    turn.emitError({ error: err, message: "codex turn failed" });
    return;
  } finally {
    relay?.close();
  }
  emit({
    type: "finish",
    finishReason: { unified: "stop", raw: "stop" },
    totalUsage: turnUsage ?? defaultUsage()
  });
}
async function startToolRelay({
  tools,
  emit,
  requestToolResult
}) {
  return startAuthorizedToolRelay({ tools, emit, requestToolResult });
}
function parseArgs(args2) {
  const out = {};
  for (let i = 0; i < args2.length; i++) {
    if (args2[i] === "--workdir" && i + 1 < args2.length) {
      out.workdir = args2[++i];
    } else if (args2[i] === "--bridge-state-dir" && i + 1 < args2.length) {
      out.bridgeStateDir = args2[++i];
    } else if (args2[i] === "--cli-shim-dir" && i + 1 < args2.length) {
      out.cliShimDir = args2[++i];
    }
  }
  return out;
}
function emitFatal(message) {
  stdout2.write(JSON.stringify({ type: "bridge-fatal", message }) + "\n");
  process.exit(1);
}
function requireArg({
  value,
  name
}) {
  if (!value) {
    emitFatal(`Missing ${name} argument.`);
  }
  return value;
}
//# sourceMappingURL=index.mjs.map