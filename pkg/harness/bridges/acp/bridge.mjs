// ../harness/dist/bridge/index.js
import { appendFile, mkdir, writeFile } from "fs/promises";
import { existsSync, readFileSync } from "fs";
import { randomUUID } from "crypto";
import { env as procEnv, pid, stdout } from "process";
import { WebSocketServer } from "ws";
var name = "AI_HarnessBridgeCapabilityUnsupportedError";
var HarnessBridgeCapabilityUnsupportedError = class extends Error {
  constructor({
    message,
    harnessId,
    cause
  }) {
    super(message);
    Object.defineProperty(this, "name", { value: name });
    this.harnessId = harnessId;
    this.cause = cause;
  }
  static isInstance(error) {
    return error != null && typeof error === "object" && "name" in error && error.name === name && "message" in error && typeof error.message === "string";
  }
};
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
  const { bridgeType: bridgeType2, bridgeStateDir: bridgeStateDir2, onStart, onStop, onDestroy } = options;
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
          type: bridgeType2,
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
        `[harness:${bridgeType2}:error] ${input.message}: ${formatted.message}
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
          rawStderrWrite(`[harness:${bridgeType2}:warn] ${line}
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
                source: bridgeType2,
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

// src/v1/bridge/index.ts
import * as acp5 from "@agentclientprotocol/sdk";
import { spawn } from "child_process";
import { readFile, writeFile as writeFile2 } from "fs/promises";
import { Readable, Writable } from "stream";
import { argv, env as processEnv } from "process";

// src/v1/bridge/acp-v1-bridge-environment.ts
import { z } from "zod/v4";
var ACP_BRIDGE_CONFIGURATION_ENV = "AI_SDK_ACP_BRIDGE_CONFIGURATION";
var serializableValueSchema = z.json();
var profileValueSchema = z.lazy(
  () => z.union([
    z.string(),
    z.number(),
    z.boolean(),
    z.null(),
    z.object({
      $source: z.enum([
        "gateway-api-key",
        "gateway-base-url",
        "gateway-authorization",
        "client-app",
        "client-app-name",
        "client-app-version"
      ]),
      prefix: z.string().optional(),
      suffix: z.string().optional(),
      ensureSuffix: z.string().optional()
    }),
    z.array(profileValueSchema),
    z.record(z.string(), profileValueSchema)
  ])
);
var hostToolMcpTransportSchema = z.enum([
  "stdio",
  "http"
]);
var serializableRecordSchema = z.record(z.string(), serializableValueSchema);
var profileRecordSchema = z.record(z.string(), profileValueSchema);
var authenticationSchema = z.object({
  methodId: z.string(),
  meta: serializableRecordSchema.optional(),
  clientCapabilities: serializableRecordSchema.optional()
});
var providerAuthenticationSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("direct") }),
  z.object({
    type: z.literal("ai-gateway"),
    env: profileRecordSchema
  })
]);
var bridgeConfigurationSchema = z.object({
  authentication: authenticationSchema.optional(),
  providerAuthentication: providerAuthenticationSchema.optional(),
  providerEnvironment: z.record(z.string(), z.string()).optional(),
  sessionMeta: serializableRecordSchema.optional(),
  clientCapabilities: serializableRecordSchema.optional(),
  askUserQuestionsRequestMethod: z.string().optional(),
  hostToolMcpTransport: hostToolMcpTransportSchema.optional()
});
async function readACPBridgeEnvironment({
  env
}) {
  const serialized = env[ACP_BRIDGE_CONFIGURATION_ENV];
  if (serialized == null) return {};
  try {
    const result = bridgeConfigurationSchema.safeParse(JSON.parse(serialized));
    if (result.success) return result.data;
  } catch {
  }
  throw new Error("ACP bridge configuration environment is invalid.");
}

// src/v1/bridge/profile-values.ts
function resolveACPProfileValue({
  value,
  gateway
}) {
  if (Array.isArray(value)) {
    return value.map((item) => resolveACPProfileValue({ value: item, gateway }));
  }
  if (value !== null && typeof value === "object") {
    if (isValueSource(value)) {
      const sourceValue = resolveSource({ source: value.$source, gateway });
      const resolved = `${value.prefix ?? ""}${sourceValue}`;
      return value.ensureSuffix == null ? `${resolved}${value.suffix ?? ""}` : ensureSuffix({
        value: resolved,
        suffix: value.ensureSuffix
      });
    }
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        resolveACPProfileValue({ value: item, gateway })
      ])
    );
  }
  return value;
}
function isValueSource(value) {
  return "$source" in value && typeof value.$source === "string";
}
function ensureSuffix({
  value,
  suffix
}) {
  const normalized = value.replace(/\/+$/, "");
  return normalized.endsWith(suffix) ? normalized : `${normalized}${suffix}`;
}
function resolveSource({
  source,
  gateway
}) {
  switch (source) {
    case "gateway-api-key":
      return gateway.apiKey;
    case "gateway-base-url":
      return gateway.baseUrl;
    case "gateway-authorization":
      return `Bearer ${gateway.apiKey}`;
    case "client-app":
      return `${gateway.clientAppName}/${gateway.clientAppVersion}`;
    case "client-app-name":
      return gateway.clientAppName;
    case "client-app-version":
      return gateway.clientAppVersion;
  }
}

// src/v1/bridge/protocol-configuration.ts
function createACPInitializeRequest({
  protocolVersion,
  clientApp,
  authentication,
  clientCapabilities: configuredClientCapabilities,
  supportsBooleanSessionConfigOptions = false
}) {
  let clientCapabilities = mergeRecords({
    left: authentication?.clientCapabilities ?? {},
    right: configuredClientCapabilities ?? {}
  });
  if (supportsBooleanSessionConfigOptions) {
    clientCapabilities = mergeRecords({
      left: clientCapabilities,
      right: {
        session: {
          configOptions: {
            boolean: {}
          }
        }
      }
    });
  }
  return {
    protocolVersion,
    clientInfo: clientApp,
    clientCapabilities
  };
}
function resolveACPLaunchEnvironment({
  providerAuthentication,
  gateway
}) {
  if (providerAuthentication?.type !== "ai-gateway") {
    return {};
  }
  const resolved = asRecord(
    resolveACPProfileValue({
      value: providerAuthentication.env,
      gateway: requireGateway({ gateway })
    })
  );
  return Object.fromEntries(
    Object.entries(resolved).map(([key, value]) => [
      key,
      typeof value === "string" ? value : JSON.stringify(value)
    ])
  );
}
function validateACPProtocolVersion({
  requested,
  initialization
}) {
  if (initialization.protocolVersion !== requested) {
    throw new Error(
      `ACP protocol negotiation failed: requested v${requested}, agent selected v${initialization.protocolVersion}.`
    );
  }
}
function assertACPAuthenticationMethod({
  initialization,
  methodId
}) {
  if (!initialization.authMethods?.some((method) => method.id === methodId)) {
    const advertised = initialization.authMethods?.map((method) => method.id).join(", ") || "none";
    throw new Error(
      `ACP authentication method ${JSON.stringify(methodId)} is not advertised by the agent. Advertised methods: ${advertised}.`
    );
  }
}
function mergeRecords({
  left,
  right
}) {
  const result = { ...left };
  for (const [key, value] of Object.entries(right)) {
    const previous = result[key];
    result[key] = isRecord(previous) && isRecord(value) ? mergeRecords({ left: previous, right: value }) : value;
  }
  return result;
}
function requireGateway({
  gateway
}) {
  if (gateway == null) {
    throw new Error("ACP Gateway profile values are unavailable.");
  }
  return gateway;
}
function asRecord(value) {
  if (!isRecord(value)) {
    throw new Error("ACP profile data must resolve to an object.");
  }
  return value;
}
function isRecord(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

// src/v1/bridge/acp-stream-capture.ts
var KNOWN_SESSION_UPDATES = /* @__PURE__ */ new Set([
  "user_message_chunk",
  "agent_message_chunk",
  "agent_thought_chunk",
  "tool_call",
  "tool_call_update",
  "plan",
  "plan_update",
  "plan_removed",
  "available_commands_update",
  "current_mode_update",
  "config_option_update",
  "session_info_update",
  "usage_update"
]);
function captureACPStream({ stream }) {
  const updates = [];
  const readable = stream.readable.pipeThrough(
    new TransformStream({
      transform(message, controller) {
        const rawUpdate = getRawSessionUpdate({ message });
        if (!rawUpdate.isSessionUpdate) {
          controller.enqueue(message);
          return;
        }
        const sessionUpdate = getStringProperty({
          value: rawUpdate.value,
          property: "sessionUpdate"
        });
        const forwarded = !rawUpdate.canFilterUnknown || sessionUpdate == null || KNOWN_SESSION_UPDATES.has(sessionUpdate);
        updates.push({ rawUpdate: rawUpdate.value, forwarded });
        if (forwarded) controller.enqueue(message);
      }
    })
  );
  return {
    stream: { ...stream, readable },
    capture: {
      takeForUpdate: ({ update }) => {
        const index = updates.findIndex(
          (candidate) => candidate.forwarded && sessionUpdatesMatch({
            rawUpdate: candidate.rawUpdate,
            update
          })
        );
        if (index === -1) {
          return { precedingRawValues: [], rawUpdate: update };
        }
        const consumed = updates.splice(0, index + 1);
        return {
          precedingRawValues: consumed.slice(0, -1).map((candidate) => candidate.rawUpdate),
          rawUpdate: consumed.at(-1).rawUpdate
        };
      },
      drainRawValues: () => updates.splice(0).map((candidate) => candidate.rawUpdate)
    }
  };
}
function getRawSessionUpdate({ message }) {
  const record = message;
  if (record.method !== "session/update" || !isRecord2(record.params)) {
    return { isSessionUpdate: false };
  }
  const params = record.params;
  return {
    isSessionUpdate: true,
    value: params.update,
    canFilterUnknown: typeof params.sessionId === "string" && isRecord2(params.update)
  };
}
function sessionUpdatesMatch({
  rawUpdate,
  update
}) {
  if (!isRecord2(rawUpdate)) return false;
  if (rawUpdate.sessionUpdate !== update.sessionUpdate) return false;
  if (update.sessionUpdate === "tool_call" || update.sessionUpdate === "tool_call_update") {
    return rawUpdate.toolCallId === update.toolCallId;
  }
  if (update.sessionUpdate === "user_message_chunk" || update.sessionUpdate === "agent_message_chunk" || update.sessionUpdate === "agent_thought_chunk") {
    return rawUpdate.messageId === update.messageId;
  }
  return true;
}
function getStringProperty({
  value,
  property
}) {
  if (!isRecord2(value)) return void 0;
  const propertyValue = value[property];
  return typeof propertyValue === "string" ? propertyValue : void 0;
}
function isRecord2(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

// src/v1/bridge/acp-diagnostics.ts
function createACPInitializationDiagnostic({
  initialization,
  sessionId
}) {
  const agentInfo = initialization.agentInfo;
  return {
    protocolVersion: initialization.protocolVersion,
    sessionId,
    agent: agentInfo == null ? null : {
      name: agentInfo.name,
      version: agentInfo.version,
      ...agentInfo.title == null ? {} : { title: agentInfo.title }
    },
    capabilities: stripMetadata({
      value: initialization.agentCapabilities ?? {}
    }),
    authMethods: (initialization.authMethods ?? []).map((method) => ({
      id: method.id,
      type: "type" in method ? method.type : "agent"
    }))
  };
}
function createACPBridgeError({
  stage,
  cause
}) {
  const causeMessage = getErrorMessage({ error: cause });
  const error = new Error(
    causeMessage == null ? `ACP ${stage} failed.` : `ACP ${stage} failed: ${causeMessage}`
  );
  error.cause = cause;
  return error;
}
function getErrorMessage({ error }) {
  if (error instanceof Error) {
    return error.message;
  }
  if (error != null && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
}
function stripMetadata({ value }) {
  if (Array.isArray(value)) {
    return value.map((item) => stripMetadata({ value: item }));
  }
  if (value == null || typeof value !== "object") return value;
  const result = {};
  for (const [key, item] of Object.entries(value)) {
    if (key !== "_meta") result[key] = stripMetadata({ value: item });
  }
  return result;
}

// src/v1/bridge/agent-stderr-monitor.ts
import { stripVTControlCharacters } from "util";
function monitorACPAgentStderr({
  stderr,
  onStderrLine
}) {
  let rejectFailure;
  const failure = new Promise((_, reject) => {
    rejectFailure = reject;
  });
  void failure.catch(() => {
  });
  void (async () => {
    stderr.setEncoding("utf8");
    let pending = "";
    for await (const chunk of stderr) {
      pending += chunk;
      const lines = pending.split("\n");
      pending = lines.pop() ?? "";
      for (const line of lines) handleStderrLine({ line });
    }
    if (pending.length > 0) handleStderrLine({ line: pending });
  })().catch((error) => rejectFailure(error));
  return failure;
  function handleStderrLine({ line }) {
    if (line.length === 0) return;
    onStderrLine(line);
    const normalizedLine = stripVTControlCharacters(line);
    if (normalizedLine.toLowerCase().includes("failed to deserialize responsestreamevent from stream")) {
      rejectFailure(
        new Error("ACP agent failed to deserialize a streamed response.")
      );
    }
  }
}

// src/v1/bridge/stream-translator.ts
function createACPStreamTranslator({
  emit,
  emitToolCallCandidate,
  builtinTools = []
}) {
  let openBlock;
  let blockCounter = 0;
  let stepOpen = false;
  let finished = false;
  const blockIdCounts = /* @__PURE__ */ new Map();
  const toolStates = /* @__PURE__ */ new Map();
  const pendingToolCallIds = /* @__PURE__ */ new Set();
  const builtinToolsByName = indexBuiltinTools({ builtinTools });
  const closeBlock = () => {
    if (openBlock == null) return;
    emit({
      type: openBlock.type === "text" ? "text-end" : "reasoning-end",
      id: openBlock.id
    });
    openBlock = void 0;
  };
  const createBlockId = ({
    type,
    messageId
  }) => {
    const base = messageId ?? `acp-${type}-${++blockCounter}`;
    const count = (blockIdCounts.get(base) ?? 0) + 1;
    blockIdCounts.set(base, count);
    return count === 1 ? base : `${base}-${count}`;
  };
  const emitContent = ({
    type,
    text,
    messageId
  }) => {
    const normalizedMessageId = messageId ?? null;
    if (openBlock == null || openBlock.type !== type || openBlock.messageId !== normalizedMessageId) {
      closeBlock();
      const id = createBlockId({ type, messageId: normalizedMessageId });
      emit({
        type: type === "text" ? "text-start" : "reasoning-start",
        id
      });
      openBlock = { type, id, messageId: normalizedMessageId };
    }
    emit({
      type: type === "text" ? "text-delta" : "reasoning-delta",
      id: openBlock.id,
      delta: text
    });
    stepOpen = true;
  };
  const emitInferredStep = ({
    finishReason
  }) => {
    closeBlock();
    emit({
      type: "finish-step",
      finishReason,
      usage: unknownUsage(),
      harnessMetadata: { acp: { inferredStep: true } }
    });
    stepOpen = false;
  };
  const finishCompletedToolStep = () => {
    if (!stepOpen || pendingToolCallIds.size > 0) {
      return;
    }
    emitInferredStep({
      finishReason: { unified: "tool-calls", raw: "tool-calls" }
    });
  };
  const emitToolUpdate = ({
    update,
    rawUpdate,
    forceEmit = false
  }) => {
    closeBlock();
    const state = toolStates.get(update.toolCallId) ?? {
      toolCallId: update.toolCallId,
      values: {},
      emittedFilePaths: /* @__PURE__ */ new Set(),
      emittedCall: false,
      emittedResult: false
    };
    toolStates.set(update.toolCallId, state);
    mergeToolUpdate({ state, update, rawUpdate });
    if (!forceEmit && !state.emittedCall && update.sessionUpdate === "tool_call" && update.status === "pending") {
      return;
    }
    if (!state.emittedCall) {
      const programmaticName = getStringProperty2({
        value: state.values,
        property: "name"
      });
      const builtin = resolveBuiltinTool({
        programmaticName,
        metadata: state.values._meta,
        title: getStringProperty2({
          value: state.values,
          property: "title"
        }),
        kind: isACPToolKind(state.values.kind) ? state.values.kind : void 0,
        rawInput: state.values.rawInput,
        builtinTools,
        builtinToolsByName
      });
      emitToolCallCandidate?.({ toolCall: createACPToolCall({ state }) });
      if (!forceEmit && builtin?.inputSchema != null && state.values.status !== "completed" && state.values.status !== "failed" && !hasRequiredBuiltinToolInput({
        rawInput: state.values.rawInput,
        inputSchema: builtin.inputSchema
      })) {
        return;
      }
      if (builtin != null) {
        state.toolName = builtin.toolName;
        state.nativeName = builtin.nativeName != null && builtin.toolName !== builtin.nativeName ? builtin.nativeName : void 0;
      } else {
        state.toolName = createDynamicToolName({
          programmaticName,
          toolCallId: state.toolCallId
        });
      }
      emit({
        type: "tool-call",
        toolCallId: state.toolCallId,
        toolName: state.toolName,
        input: stringifyToolInput({ input: state.values.rawInput }),
        providerExecuted: true,
        ...state.nativeName == null ? {} : { nativeName: state.nativeName }
      });
      state.emittedCall = true;
      stepOpen = true;
      pendingToolCallIds.add(state.toolCallId);
    }
    emitFileChanges({ state, emit });
    if (!state.emittedResult && (state.values.status === "completed" || state.values.status === "failed")) {
      emit({
        type: "tool-result",
        toolCallId: state.toolCallId,
        toolName: state.toolName,
        result: createToolResult({ state }),
        ...state.values.status === "failed" ? { isError: true } : {}
      });
      state.emittedResult = true;
      pendingToolCallIds.delete(state.toolCallId);
      finishCompletedToolStep();
    }
  };
  const hostToolCall = ({
    toolCallId,
    toolName,
    input
  }) => {
    closeBlock();
    emit({
      type: "tool-call",
      toolCallId,
      toolName,
      input: stringifyToolInput({ input }),
      providerExecuted: false
    });
    stepOpen = true;
    pendingToolCallIds.add(toolCallId);
  };
  const hostToolResult = ({
    toolCallId,
    toolName,
    output,
    isError
  }) => {
    closeBlock();
    emit({
      type: "tool-result",
      toolCallId,
      toolName,
      result: toSafeJSONValue({ value: output, fallback: {} }) ?? {},
      ...isError ? { isError: true } : {}
    });
    pendingToolCallIds.delete(toolCallId);
    finishCompletedToolStep();
  };
  return {
    update: (options) => {
      const { update, rawUpdate, preserveRaw } = "update" in options ? {
        update: options.update,
        rawUpdate: options.rawUpdate,
        preserveRaw: options.preserveRaw
      } : { update: options, rawUpdate: options, preserveRaw: true };
      const preservedUpdate = rawUpdate === void 0 ? update : rawUpdate;
      if (preserveRaw !== false) {
        emit({ type: "raw", rawValue: preservedUpdate });
      }
      if (update.sessionUpdate === "agent_message_chunk" && update.content?.type === "text" && typeof update.content.text === "string") {
        emitContent({
          type: "text",
          text: update.content.text,
          messageId: update.messageId
        });
        return;
      }
      if (update.sessionUpdate === "agent_thought_chunk" && update.content?.type === "text" && typeof update.content.text === "string") {
        emitContent({
          type: "reasoning",
          text: update.content.text,
          messageId: update.messageId
        });
        return;
      }
      if (update.sessionUpdate === "agent_message_chunk" || update.sessionUpdate === "agent_thought_chunk") {
        closeBlock();
        return;
      }
      if (update.sessionUpdate === "tool_call" || update.sessionUpdate === "tool_call_update") {
        emitToolUpdate({ update, rawUpdate: preservedUpdate });
      }
    },
    permissionToolCall: ({ toolCall }) => {
      emitToolUpdate({
        update: toolCall,
        rawUpdate: toolCall,
        forceEmit: true
      });
    },
    raw: ({ rawValue }) => {
      emit({ type: "raw", rawValue });
    },
    close: closeBlock,
    hostToolCall,
    hostToolResult,
    getToolCall: ({ toolCallId }) => {
      const state = toolStates.get(toolCallId);
      return state == null ? void 0 : createACPToolCall({ state });
    },
    finish: (response) => {
      if (finished) return;
      finished = true;
      emit({ type: "raw", rawValue: response });
      const finishReason = mapACPFinishReason({
        stopReason: response.stopReason
      });
      const usageMetadata = response.usage == null ? void 0 : mapACPUsageMetadata({ usage: response.usage });
      if (stepOpen) {
        emitInferredStep({ finishReason });
      } else {
        closeBlock();
      }
      emit({
        type: "finish",
        finishReason,
        totalUsage: mapACPUsage({ usage: response.usage }),
        harnessMetadata: {
          acp: {
            stopReason: response.stopReason,
            ...usageMetadata == null ? {} : { usage: usageMetadata }
          }
        }
      });
    }
  };
}
function mapACPFinishReason({ stopReason }) {
  switch (stopReason) {
    case "end_turn":
      return { unified: "stop", raw: stopReason };
    case "max_tokens":
    case "max_turn_requests":
      return { unified: "length", raw: stopReason };
    case "refusal":
      return { unified: "content-filter", raw: stopReason };
    default:
      return { unified: "other", raw: stopReason };
  }
}
function indexBuiltinTools({
  builtinTools
}) {
  const matches = /* @__PURE__ */ new Map();
  for (const builtinTool of builtinTools) {
    addBuiltinToolMatch({
      matches,
      name: builtinTool.toolName,
      builtinTool
    });
    if (builtinTool.nativeName != null) {
      addBuiltinToolMatch({
        matches,
        name: builtinTool.nativeName,
        builtinTool
      });
    }
  }
  return new Map(
    [...matches].filter(
      (entry) => entry[1] != null
    )
  );
}
function addBuiltinToolMatch({
  matches,
  name: name2,
  builtinTool
}) {
  if (!matches.has(name2)) {
    matches.set(name2, builtinTool);
    return;
  }
  if (matches.get(name2)?.toolName !== builtinTool.toolName) {
    matches.set(name2, null);
  }
}
function resolveBuiltinTool({
  programmaticName,
  metadata,
  title,
  kind,
  rawInput,
  builtinTools,
  builtinToolsByName
}) {
  if (programmaticName != null) {
    return builtinToolsByName.get(programmaticName);
  }
  const metadataMatch = findBuiltinToolsInMetadata({
    metadata,
    builtinToolsByName
  });
  if (metadataMatch.hasProgrammaticName) {
    return metadataMatch.matches.length === 1 ? metadataMatch.matches[0] : void 0;
  }
  const titleMatch = findBuiltinToolByTitle({
    title,
    kind,
    builtinTools
  });
  if (titleMatch != null) return titleMatch;
  const schemaMatches = builtinTools.filter(
    (tool) => matchesBuiltinToolInput({ rawInput, inputSchema: tool.inputSchema })
  );
  if (schemaMatches.length <= 1) return schemaMatches[0];
  return findMostSpecificSchemaMatch({ rawInput, schemaMatches });
}
function findMostSpecificSchemaMatch({
  rawInput,
  schemaMatches
}) {
  if (!isRecord3(rawInput)) return void 0;
  const rawInputKeys = Object.keys(rawInput);
  const coverageOf = (tool) => {
    const properties = isRecord3(tool.inputSchema) ? isRecord3(tool.inputSchema.properties) ? tool.inputSchema.properties : {} : {};
    return rawInputKeys.filter((key) => key in properties).length;
  };
  const highestCoverage = Math.max(...schemaMatches.map(coverageOf));
  const mostSpecificMatches = schemaMatches.filter(
    (tool) => coverageOf(tool) === highestCoverage
  );
  return mostSpecificMatches.length === 1 ? mostSpecificMatches[0] : void 0;
}
function findBuiltinToolByTitle({
  title,
  kind,
  builtinTools
}) {
  if (title == null) return void 0;
  const matches = builtinTools.filter(
    (tool) => tool.title != null && title.startsWith(tool.title) && isCompatibleToolKind({ toolUseKind: tool.toolUseKind, kind })
  );
  if (matches.length === 0) return void 0;
  const longestPrefixLength = Math.max(
    ...matches.map((tool) => tool.title.length)
  );
  const longestMatches = matches.filter(
    (tool) => tool.title.length === longestPrefixLength
  );
  return longestMatches.length === 1 ? longestMatches[0] : void 0;
}
function isCompatibleToolKind({
  toolUseKind,
  kind
}) {
  if (toolUseKind == null || kind == null) return true;
  switch (toolUseKind) {
    case "readonly":
      return kind === "read" || kind === "search" || kind === "fetch" || kind === "think";
    case "edit":
      return kind === "edit" || kind === "delete" || kind === "move";
    case "bash":
      return kind === "execute";
  }
}
function findBuiltinToolsInMetadata({
  metadata,
  builtinToolsByName
}) {
  const matches = /* @__PURE__ */ new Map();
  let hasProgrammaticName = false;
  const seen = /* @__PURE__ */ new Set();
  const visit = (value) => {
    if (value == null || typeof value !== "object" || seen.has(value)) return;
    seen.add(value);
    for (const [property, child2] of Array.isArray(value) ? value.entries() : Object.entries(value)) {
      if ((property === "name" || property === "toolName") && typeof child2 === "string") {
        hasProgrammaticName = true;
        const builtin = builtinToolsByName.get(child2);
        if (builtin != null) matches.set(builtin.toolName, builtin);
      }
      visit(child2);
    }
  };
  visit(metadata);
  return { hasProgrammaticName, matches: [...matches.values()] };
}
function matchesBuiltinToolInput({
  rawInput,
  inputSchema
}) {
  if (!isRecord3(rawInput) || !isRecord3(inputSchema)) return false;
  if (inputSchema.type !== "object") return false;
  const required = inputSchema.required;
  if (!Array.isArray(required) || required.length === 0 || !required.every((property) => typeof property === "string")) {
    return false;
  }
  if (!hasRequiredBuiltinToolInput({ rawInput, inputSchema })) return false;
  const properties = isRecord3(inputSchema.properties) ? inputSchema.properties : {};
  return Object.entries(properties).filter(([, schema]) => isJSONSchemaDiscriminator({ schema })).every(([property, schema]) => {
    if (!Object.prototype.hasOwnProperty.call(rawInput, property)) {
      return false;
    }
    return matchesJSONSchemaValue({ value: rawInput[property], schema });
  });
}
function hasRequiredBuiltinToolInput({
  rawInput,
  inputSchema
}) {
  if (!isRecord3(inputSchema) || inputSchema.type !== "object") return true;
  if (!isRecord3(rawInput)) return false;
  const required = inputSchema.required;
  if (!Array.isArray(required) || required.length === 0) return true;
  if (!required.every((property) => typeof property === "string")) return true;
  const properties = isRecord3(inputSchema.properties) ? inputSchema.properties : {};
  return required.every((property) => {
    if (!Object.prototype.hasOwnProperty.call(rawInput, property)) return false;
    return matchesJSONSchemaValue({
      value: rawInput[property],
      schema: properties[property]
    });
  });
}
function isJSONSchemaDiscriminator({ schema }) {
  return isRecord3(schema) && ("const" in schema || Array.isArray(schema.enum) && schema.enum.length === 1);
}
function matchesJSONSchemaValue({
  value,
  schema
}) {
  if (schema === true) return true;
  if (schema === false || !isRecord3(schema)) return false;
  if ("const" in schema && !Object.is(value, schema.const)) return false;
  if (Array.isArray(schema.enum) && !schema.enum.some((candidate) => Object.is(value, candidate))) {
    return false;
  }
  for (const alternatives of [schema.anyOf, schema.oneOf]) {
    if (Array.isArray(alternatives) && !alternatives.some(
      (alternative) => matchesJSONSchemaValue({ value, schema: alternative })
    )) {
      return false;
    }
  }
  if (typeof schema.type !== "string") return true;
  switch (schema.type) {
    case "null":
      return value === null;
    case "boolean":
      return typeof value === "boolean";
    case "integer":
      return typeof value === "number" && Number.isInteger(value);
    case "number":
      return typeof value === "number" && Number.isFinite(value);
    case "string":
      return typeof value === "string";
    case "array":
      return Array.isArray(value);
    case "object":
      return isRecord3(value);
    default:
      return true;
  }
}
function mergeToolUpdate({
  state,
  update,
  rawUpdate
}) {
  const parsed = update;
  for (const property of [
    "title",
    "kind",
    "status",
    "content",
    "locations",
    "rawInput",
    "rawOutput",
    "_meta"
  ]) {
    if (Object.prototype.hasOwnProperty.call(parsed, property) && parsed[property] !== void 0) {
      state.values[property] = parsed[property];
    }
  }
  const programmaticName = getStringProperty2({
    value: rawUpdate,
    property: "name"
  });
  if (programmaticName != null) state.values.name = programmaticName;
}
function createACPToolCall({ state }) {
  const title = getStringProperty2({
    value: state.values,
    property: "title"
  });
  const kind = state.values.kind;
  const status = state.values.status;
  return {
    toolCallId: state.toolCallId,
    title: title ?? state.toolName ?? `Tool ${state.toolCallId}`,
    ...isACPToolKind(kind) ? { kind } : {},
    ...isACPToolCallStatus(status) ? { status } : {},
    ...Array.isArray(state.values.content) ? { content: state.values.content } : {},
    ...Array.isArray(state.values.locations) ? { locations: state.values.locations } : {},
    ...Object.prototype.hasOwnProperty.call(state.values, "rawInput") ? { rawInput: state.values.rawInput } : {},
    ...Object.prototype.hasOwnProperty.call(state.values, "rawOutput") ? { rawOutput: state.values.rawOutput } : {},
    ...state.values._meta === null || isRecord3(state.values._meta) ? { _meta: state.values._meta } : {}
  };
}
function isACPToolKind(value) {
  return value === "read" || value === "edit" || value === "delete" || value === "move" || value === "search" || value === "execute" || value === "think" || value === "fetch" || value === "switch_mode" || value === "other";
}
function isACPToolCallStatus(value) {
  return value === "pending" || value === "in_progress" || value === "completed" || value === "failed";
}
function emitFileChanges({
  state,
  emit
}) {
  const content = Array.isArray(state.values.content) ? state.values.content : [];
  for (const item of content) {
    if (!isRecord3(item) || item.type !== "diff") continue;
    const path = getStringProperty2({ value: item, property: "path" });
    const newText = getStringProperty2({ value: item, property: "newText" });
    if (path == null || newText == null) continue;
    const oldText = item.oldText;
    const event = oldText == null ? "create" : "modify";
    emitFileChange({ state, emit, path, event });
  }
  if (state.values.kind !== "edit") return;
  const locations = Array.isArray(state.values.locations) ? state.values.locations : [];
  for (const location of locations) {
    const path = getStringProperty2({ value: location, property: "path" });
    if (path != null) {
      emitFileChange({ state, emit, path, event: "modify" });
    }
  }
}
function emitFileChange({
  state,
  emit,
  path,
  event
}) {
  if (state.emittedFilePaths.has(path)) return;
  state.emittedFilePaths.add(path);
  emit({
    type: "file-change",
    path,
    event,
    harnessMetadata: {
      acp: {
        toolCallId: state.toolCallId
      }
    }
  });
}
function createDynamicToolName({
  programmaticName,
  toolCallId
}) {
  const source = programmaticName ?? `acp_tool_${toolCallId}`;
  const normalized = source.replace(/[^A-Za-z0-9_-]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 128);
  if (normalized.length === 0) return "acp_tool";
  return /^[A-Za-z_]/.test(normalized) ? normalized : `acp_${normalized}`;
}
function stringifyToolInput({ input }) {
  return JSON.stringify(toSafeJSONValue({ value: input, fallback: {} }));
}
function createToolResult({ state }) {
  const value = state.values.rawOutput != null ? state.values.rawOutput : state.values.content != null ? state.values.content : {};
  return toSafeJSONValue({ value, fallback: {} }) ?? {};
}
function toSafeJSONValue({
  value,
  fallback,
  seen = /* @__PURE__ */ new Set()
}) {
  if (value == null) return fallback;
  if (typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value === "bigint") return value.toString();
  if (typeof value !== "object") return fallback;
  if (seen.has(value)) return "[Circular]";
  seen.add(value);
  if (Array.isArray(value)) {
    const result2 = value.map(
      (item) => toSafeJSONValue({ value: item, fallback: null, seen })
    );
    seen.delete(value);
    return result2;
  }
  const result = /* @__PURE__ */ Object.create(null);
  try {
    for (const [key, item] of Object.entries(value)) {
      result[key] = toSafeJSONValue({
        value: item,
        fallback: null,
        seen
      });
    }
  } catch {
    seen.delete(value);
    return fallback;
  }
  seen.delete(value);
  return result;
}
function getStringProperty2({
  value,
  property
}) {
  if (!isRecord3(value)) return void 0;
  const propertyValue = value[property];
  return typeof propertyValue === "string" ? propertyValue : void 0;
}
function isRecord3(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}
function mapACPUsage({
  usage
}) {
  if (usage == null) return unknownUsage();
  const raw = mapACPUsageMetadata({ usage });
  return {
    inputTokens: {
      total: usage.inputTokens,
      noCache: void 0,
      cacheRead: usage.cachedReadTokens ?? void 0,
      cacheWrite: usage.cachedWriteTokens ?? void 0
    },
    outputTokens: {
      total: usage.outputTokens,
      text: void 0,
      reasoning: usage.thoughtTokens ?? void 0
    },
    raw
  };
}
function mapACPUsageMetadata({
  usage
}) {
  return {
    totalTokens: usage.totalTokens,
    inputTokens: usage.inputTokens,
    outputTokens: usage.outputTokens,
    ...usage.thoughtTokens == null ? {} : { thoughtTokens: usage.thoughtTokens },
    ...usage.cachedReadTokens == null ? {} : { cachedReadTokens: usage.cachedReadTokens },
    ...usage.cachedWriteTokens == null ? {} : { cachedWriteTokens: usage.cachedWriteTokens }
  };
}
function unknownUsage() {
  return {
    inputTokens: {
      total: void 0,
      noCache: void 0,
      cacheRead: void 0,
      cacheWrite: void 0
    },
    outputTokens: {
      total: void 0,
      text: void 0,
      reasoning: void 0
    }
  };
}

// src/v1/bridge/host-tool-correlation.ts
var CORRELATION_WINDOW_MS = 1e3;
var MAX_BUFFERED_UPDATES = 128;
function createHostToolCorrelation({
  emitSemanticUpdate,
  emitRawUpdate,
  hostToolServerName,
  hostTools
}) {
  const invocations = /* @__PURE__ */ new Map();
  const candidates = /* @__PURE__ */ new Map();
  const suppressedToolCallIds = /* @__PURE__ */ new Set();
  const releasedToolCallIds = /* @__PURE__ */ new Set();
  const observedToolCalls = /* @__PURE__ */ new Map();
  let nextCandidateOrder = 0;
  let buffered = [];
  let flushTimer;
  const flush = () => {
    if (flushTimer != null) {
      clearTimeout(flushTimer);
      flushTimer = void 0;
    }
    if (buffered.length === 0) return;
    const updates = buffered;
    buffered = [];
    for (const update of updates) {
      if (isToolUpdate(update.message) && suppressedToolCallIds.has(update.message.update.toolCallId)) {
        continue;
      }
      if (isToolUpdate(update.message)) {
        releasedToolCallIds.add(update.message.update.toolCallId);
      }
      emitSemanticUpdate(update);
    }
    for (const toolCallId of candidates.keys()) {
      if (!suppressedToolCallIds.has(toolCallId)) {
        releasedToolCallIds.add(toolCallId);
      }
    }
    candidates.clear();
  };
  const scheduleFlush = () => {
    if (flushTimer != null) return;
    flushTimer = setTimeout(flush, CORRELATION_WINDOW_MS);
    flushTimer.unref?.();
  };
  const suppressCandidate = ({
    candidate,
    invocation
  }) => {
    invocation.toolCallId = candidate.toolCallId;
    suppressedToolCallIds.add(candidate.toolCallId);
    candidates.delete(candidate.toolCallId);
    deleteInvocation({ invocations, token: invocation.token });
  };
  const suppressToolCallUpdate = ({ toolCallId }) => {
    suppressedToolCallIds.add(toolCallId);
    candidates.delete(toolCallId);
    if (candidates.size === 0) flush();
  };
  const reconcile = () => {
    const orderedInvocations = [...invocations.values()].sort(
      (left, right) => left.order - right.order
    );
    const orderedCandidates = [...candidates.values()].sort(
      (left, right) => left.order - right.order
    );
    for (const invocation of orderedInvocations) {
      if (invocation.toolCallId != null) continue;
      const candidate = orderedCandidates.find(
        (item) => candidates.has(item.toolCallId) && hasPortableEvidence({ candidate: item, invocation })
      );
      if (candidate != null) suppressCandidate({ candidate, invocation });
    }
    if (candidates.size === 0) flush();
  };
  return {
    update: ({ message, rawUpdate }) => {
      const preservedUpdate = rawUpdate === void 0 ? message.update : rawUpdate;
      emitRawUpdate({ rawUpdate: preservedUpdate });
      if (!isToolUpdate(message)) {
        if (buffered.length === 0) {
          emitSemanticUpdate({ message, rawUpdate: preservedUpdate });
        } else {
          buffered.push({ message, rawUpdate: preservedUpdate });
          if (buffered.length >= MAX_BUFFERED_UPDATES) flush();
        }
        return;
      }
      const toolCallId = message.update.toolCallId;
      observedToolCalls.set(
        toolCallId,
        mergeObservedToolCall({
          previous: observedToolCalls.get(toolCallId),
          update: message.update
        })
      );
      if (suppressedToolCallIds.has(toolCallId)) {
        if (isTerminalToolUpdate(message)) {
          suppressedToolCallIds.delete(toolCallId);
          removeInvocationForToolCall({ invocations, toolCallId });
        }
        return;
      }
      if (releasedToolCallIds.has(toolCallId)) {
        emitSemanticUpdate({ message, rawUpdate: preservedUpdate });
        if (isTerminalToolUpdate(message))
          releasedToolCallIds.delete(toolCallId);
        return;
      }
      buffered.push({ message, rawUpdate: preservedUpdate });
      const candidate = candidates.get(toolCallId) ?? {
        toolCallId,
        order: ++nextCandidateOrder,
        evidence: []
      };
      candidate.evidence.push(message.update, preservedUpdate);
      candidates.set(toolCallId, candidate);
      const tokenMatch = findTokenInvocation({
        value: preservedUpdate,
        invocations
      });
      if (tokenMatch != null) {
        suppressCandidate({ candidate, invocation: tokenMatch });
      }
      reconcile();
      if (buffered.length >= MAX_BUFFERED_UPDATES) flush();
      else if (buffered.length > 0) scheduleFlush();
    },
    registerInvocation: ({ token, serverName, toolName, input, order }) => {
      const invocation = {
        token,
        serverName,
        toolName,
        inputFingerprint: canonicalFingerprint({ value: input }),
        order
      };
      invocations.set(token, invocation);
      invocation.expiryTimer = setTimeout(
        () => deleteInvocation({ invocations, token }),
        CORRELATION_WINDOW_MS
      );
      invocation.expiryTimer.unref?.();
      const tokenCandidate = [...candidates.values()].find(
        (candidate) => candidate.evidence.some(
          (value) => containsExactValue({ value, target: token })
        )
      );
      if (tokenCandidate != null) {
        suppressCandidate({ candidate: tokenCandidate, invocation });
      }
      reconcile();
    },
    claimHostToolPermission: ({ toolCall }) => {
      const candidate = candidates.get(toolCall.toolCallId);
      const matches = hostTools.filter(({ name: name2 }) => {
        const permission = resolvePermissionHostTool({
          toolCall,
          serverName: hostToolServerName,
          toolName: name2
        });
        if (permission == null) return false;
        if (candidate == null) return permission.hasRequestIdentity;
        return hasPortableEvidence({
          candidate,
          invocation: {
            token: "",
            serverName: hostToolServerName,
            toolName: name2,
            inputFingerprint: canonicalFingerprint({
              value: permission.input
            }),
            order: 0
          }
        });
      });
      if (matches.length !== 1) return false;
      suppressToolCallUpdate({ toolCallId: toolCall.toolCallId });
      return true;
    },
    suppressToolCall: suppressToolCallUpdate,
    getToolCall: ({ toolCallId }) => observedToolCalls.get(toolCallId),
    flush,
    removeInvocation: ({ token }) => {
      deleteInvocation({ invocations, token });
    },
    close: () => {
      flush();
      for (const invocation of invocations.values()) {
        if (invocation.expiryTimer != null) {
          clearTimeout(invocation.expiryTimer);
        }
      }
      invocations.clear();
      candidates.clear();
      suppressedToolCallIds.clear();
      releasedToolCallIds.clear();
      observedToolCalls.clear();
    }
  };
}
function mergeObservedToolCall({
  previous,
  update
}) {
  return {
    toolCallId: update.toolCallId,
    title: update.title ?? previous?.title ?? `Tool ${update.toolCallId}`,
    ...update.kind == null ? {} : { kind: update.kind },
    ...update.status == null ? {} : { status: update.status },
    ...update.content == null ? {} : { content: update.content },
    ...update.locations == null ? {} : { locations: update.locations },
    ...update.rawInput === void 0 ? {} : { rawInput: update.rawInput },
    ...update.rawOutput === void 0 ? {} : { rawOutput: update.rawOutput },
    ...update._meta === void 0 ? {} : { _meta: update._meta }
  };
}
function hasPortableEvidence({
  candidate,
  invocation
}) {
  let hasServerIdentity = false;
  let hasToolName = false;
  let hasInput = false;
  for (const evidence of candidate.evidence) {
    const metadata = getProperty({ value: evidence, property: "_meta" });
    const rawInput = getProperty({ value: evidence, property: "rawInput" });
    const programmaticName = getProperty({
      value: evidence,
      property: "name"
    });
    const deferredToolName = getProperty({
      value: rawInput,
      property: "tool_name"
    });
    const deferredToolInput = getProperty({
      value: rawInput,
      property: "tool_input"
    });
    if (containsCombinedIdentity({
      value: [metadata, programmaticName],
      serverName: invocation.serverName,
      toolName: invocation.toolName
    })) {
      hasServerIdentity = true;
      hasToolName = true;
    }
    if (typeof deferredToolName === "string" && isRecord4(deferredToolInput) && hasDelimitedPair({
      value: deferredToolName,
      serverName: invocation.serverName,
      toolName: invocation.toolName
    })) {
      hasServerIdentity = true;
      hasToolName = true;
      if (canonicalFingerprint({ value: deferredToolInput }) === invocation.inputFingerprint) {
        hasInput = true;
      }
    }
    if (containsExactValue({
      value: [metadata, rawInput],
      target: invocation.serverName
    })) {
      hasServerIdentity = true;
    }
    if (programmaticName === invocation.toolName || containsExactValue({
      value: [metadata, rawInput],
      target: invocation.toolName
    })) {
      hasToolName = true;
    }
    if (containsFingerprint({
      value: [metadata, rawInput],
      target: invocation.inputFingerprint
    })) {
      hasInput = true;
    }
  }
  return hasServerIdentity && hasToolName && hasInput;
}
function resolvePermissionHostTool({
  toolCall,
  serverName,
  toolName
}) {
  if (!isRecord4(toolCall.rawInput)) return void 0;
  const deferredToolName = getProperty({
    value: toolCall.rawInput,
    property: "tool_name"
  });
  const deferredToolInput = getProperty({
    value: toolCall.rawInput,
    property: "tool_input"
  });
  if (hasOwnProperty({ value: toolCall.rawInput, property: "tool_name" }) && hasOwnProperty({ value: toolCall.rawInput, property: "tool_input" })) {
    if (typeof deferredToolName !== "string" || !isRecord4(deferredToolInput) || !hasDelimitedPair({
      value: deferredToolName,
      serverName,
      toolName
    })) {
      return void 0;
    }
    return {
      input: deferredToolInput,
      hasRequestIdentity: true
    };
  }
  return {
    input: toolCall.rawInput,
    hasRequestIdentity: containsCombinedIdentity({
      value: [
        getProperty({ value: toolCall, property: "title" }),
        getProperty({ value: toolCall, property: "name" }),
        getProperty({ value: toolCall, property: "_meta" })
      ],
      serverName,
      toolName
    })
  };
}
function containsCombinedIdentity({
  value,
  serverName,
  toolName
}) {
  const seen = /* @__PURE__ */ new Set();
  const visit = (candidate) => {
    if (typeof candidate === "string") {
      return hasDelimitedPair({ value: candidate, serverName, toolName });
    }
    if (candidate == null || typeof candidate !== "object") return false;
    if (seen.has(candidate)) return false;
    seen.add(candidate);
    const values = Array.isArray(candidate) ? candidate : Object.values(candidate);
    for (const item of values) {
      if (visit(item)) return true;
    }
    return false;
  };
  return visit(value);
}
function hasDelimitedPair({
  value,
  serverName,
  toolName
}) {
  if (serverName.length === 0 || toolName.length === 0) return false;
  let serverIndex = value.indexOf(serverName);
  while (serverIndex !== -1) {
    const beforeServer = serverIndex === 0 ? void 0 : value[serverIndex - 1];
    const serverHasLeadingBoundary = beforeServer == null || value.slice(0, serverIndex).endsWith("__") || !/[A-Za-z0-9_-]/.test(beforeServer);
    const delimiterStart = serverIndex + serverName.length;
    let toolIndex = delimiterStart;
    while (toolIndex < value.length && !/[A-Za-z0-9-]/.test(value[toolIndex])) {
      toolIndex += 1;
    }
    const toolEnd = toolIndex + toolName.length;
    const afterTool = toolEnd === value.length ? void 0 : value[toolEnd];
    const toolHasTrailingBoundary = afterTool == null || value.startsWith("__", toolEnd) || !/[A-Za-z0-9_-]/.test(afterTool);
    if (serverHasLeadingBoundary && toolIndex > delimiterStart && value.startsWith(toolName, toolIndex) && toolHasTrailingBoundary) {
      return true;
    }
    serverIndex = value.indexOf(serverName, serverIndex + 1);
  }
  return false;
}
function findTokenInvocation({
  value,
  invocations
}) {
  for (const invocation of invocations.values()) {
    if (containsExactValue({ value, target: invocation.token })) {
      return invocation;
    }
  }
}
function removeInvocationForToolCall({
  invocations,
  toolCallId
}) {
  for (const [token, invocation] of invocations) {
    if (invocation.toolCallId === toolCallId) {
      deleteInvocation({ invocations, token });
    }
  }
}
function deleteInvocation({
  invocations,
  token
}) {
  const invocation = invocations.get(token);
  if (invocation?.expiryTimer != null) clearTimeout(invocation.expiryTimer);
  invocations.delete(token);
}
function isToolUpdate(message) {
  return message.update.sessionUpdate === "tool_call" || message.update.sessionUpdate === "tool_call_update";
}
function containsExactValue({
  value,
  target
}) {
  const seen = /* @__PURE__ */ new Set();
  const visit = (candidate) => {
    if (candidate === target) return true;
    if (candidate == null || typeof candidate !== "object") return false;
    if (seen.has(candidate)) return false;
    seen.add(candidate);
    const values = Array.isArray(candidate) ? candidate : Object.values(candidate);
    for (const item of values) {
      if (visit(item)) return true;
    }
    return false;
  };
  return visit(value);
}
function containsFingerprint({
  value,
  target
}) {
  const seen = /* @__PURE__ */ new Set();
  const visit = (candidate) => {
    if (candidate != null && typeof candidate === "object") {
      if (canonicalFingerprint({ value: candidate }) === target) return true;
      if (seen.has(candidate)) return false;
      seen.add(candidate);
      const values = Array.isArray(candidate) ? candidate : Object.values(candidate);
      for (const item of values) {
        if (visit(item)) return true;
      }
    }
    return false;
  };
  return visit(value);
}
function canonicalFingerprint({ value }) {
  return JSON.stringify(canonicalizeJSON({ value }));
}
function canonicalizeJSON({ value }) {
  if (Array.isArray(value)) {
    return value.map((item) => canonicalizeJSON({ value: item }));
  }
  if (value == null || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.keys(value).sort().filter((key) => value[key] !== void 0).map((key) => [
      key,
      canonicalizeJSON({
        value: value[key]
      })
    ])
  );
}
function getProperty({
  value,
  property
}) {
  if (value == null || typeof value !== "object" || Array.isArray(value)) {
    return void 0;
  }
  return Reflect.get(value, property);
}
function hasOwnProperty({
  value,
  property
}) {
  return Object.prototype.hasOwnProperty.call(value, property);
}
function isRecord4(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}
function isTerminalToolUpdate(message) {
  return isToolUpdate(message) && (message.update.status === "completed" || message.update.status === "failed");
}

// src/v1/bridge/create-emit-stream-event.ts
function createEmitStreamEvent({
  emit,
  emitToolCallCandidate,
  builtinTools,
  hostToolServerName,
  hostTools
}) {
  const translator = createACPStreamTranslator({
    emit,
    emitToolCallCandidate,
    builtinTools
  });
  const correlation = createHostToolCorrelation({
    emitSemanticUpdate: ({ message, rawUpdate }) => {
      translator.update({
        update: message.update,
        rawUpdate,
        preserveRaw: false
      });
    },
    emitRawUpdate: ({ rawUpdate }) => {
      translator.raw({ rawValue: rawUpdate });
    },
    hostToolServerName,
    hostTools
  });
  return {
    message: ({ message, rawUpdate }) => {
      if (message.kind === "stop") {
        correlation.close();
        translator.finish(message.response);
        return true;
      }
      correlation.update({ message, rawUpdate });
      return false;
    },
    raw: translator.raw,
    close: () => {
      correlation.close();
      translator.close();
    },
    permissionToolCall: ({ toolCall }) => {
      translator.permissionToolCall({
        toolCall: {
          sessionUpdate: "tool_call_update",
          ...toolCall
        }
      });
    },
    claimHostToolPermission: correlation.claimHostToolPermission,
    hostToolCall: translator.hostToolCall,
    hostToolResult: translator.hostToolResult,
    registerHostToolCorrelationInvocation: correlation.registerInvocation,
    removeHostToolCorrelationInvocation: correlation.removeInvocation,
    suppressToolCall: correlation.suppressToolCall,
    getToolCall: ({ toolCallId }) => correlation.getToolCall({ toolCallId }) ?? translator.getToolCall({ toolCallId })
  };
}

// src/v1/bridge/instruction-mapping.ts
import { z as z2 } from "zod/v4";
var UNSAFE_PATH_SEGMENTS = /* @__PURE__ */ new Set(["__proto__", "constructor", "prototype"]);
var serializableRecordSchema2 = z2.record(z2.string(), z2.json());
async function resolveACPInstructionConfiguration({
  instructions,
  instructionMapping,
  sessionMeta,
  environment
}) {
  const resolvedEnvironment = { ...environment };
  if (instructionMapping == null || instructions == null || instructions.length === 0) {
    return { sessionMeta, environment: resolvedEnvironment };
  }
  if (instructionMapping.type === "filesystem") {
    if (typeof instructionMapping.path !== "string" || instructionMapping.path.trim().length === 0) {
      throw new Error(
        "ACP instruction mapping filesystem path must be a non-empty string."
      );
    }
    return { sessionMeta, environment: resolvedEnvironment };
  }
  assertSafePath({ path: instructionMapping.path });
  if (instructionMapping.type === "session-meta") {
    return {
      sessionMeta: setStringAtPath({
        record: sessionMeta ?? {},
        path: instructionMapping.path,
        value: instructions
      }),
      environment: resolvedEnvironment
    };
  }
  const serialized = resolvedEnvironment[instructionMapping.variable];
  let configuration = {};
  if (serialized != null && serialized.length > 0) {
    configuration = parseSerializableRecord({
      serialized,
      variable: instructionMapping.variable
    });
  }
  resolvedEnvironment[instructionMapping.variable] = JSON.stringify(
    setStringAtPath({
      record: configuration,
      path: instructionMapping.path,
      value: instructions
    })
  );
  return { sessionMeta, environment: resolvedEnvironment };
}
function parseSerializableRecord({
  serialized,
  variable
}) {
  try {
    const result = serializableRecordSchema2.safeParse(JSON.parse(serialized));
    if (result.success) return result.data;
  } catch {
  }
  throw new Error(
    `ACP instruction mapping environment variable ${JSON.stringify(variable)} must contain a JSON object.`
  );
}
function setStringAtPath({
  record,
  path,
  value
}) {
  const [key, ...remainingPath] = path;
  if (key == null) return { ...record };
  return {
    ...record,
    [key]: remainingPath.length === 0 ? value : setStringAtPath({
      record: isRecord5(record[key]) ? record[key] : {},
      path: remainingPath,
      value
    })
  };
}
function assertSafePath({ path }) {
  if (path.length === 0 || path.some(
    (segment) => segment.length === 0 || UNSAFE_PATH_SEGMENTS.has(segment)
  )) {
    throw new Error(
      "ACP instruction mapping path must contain only safe, non-empty property names."
    );
  }
}
function isRecord5(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

// src/v1/bridge/host-tool-relay.ts
import { randomBytes, randomUUID as randomUUID3, timingSafeEqual } from "crypto";
import { createServer } from "http";

// src/v1/bridge/host-tool-mcp-http.ts
import { randomUUID as randomUUID2 } from "crypto";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest } from "@modelcontextprotocol/sdk/types.js";

// src/v1/bridge/host-tool-mcp-server.ts
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ErrorCode,
  ListToolsRequestSchema,
  McpError
} from "@modelcontextprotocol/sdk/types.js";
var VERSION = true ? "1.0.61" : "0.0.0-test";
function createHostToolMCPServer({
  tools,
  revision = 1,
  invoke,
  onListTools
}) {
  let catalog = createCatalog({ revision, tools });
  let catalogAcknowledgmentError;
  const server = new Server(
    {
      name: "@ai-sdk/harness-acp-host-tools",
      version: VERSION
    },
    { capabilities: { tools: { listChanged: true } } }
  );
  server.setRequestHandler(ListToolsRequestSchema, async () => {
    if (catalogAcknowledgmentError != null) {
      throw catalogAcknowledgmentError;
    }
    const current = catalog;
    if (onListTools != null) {
      setImmediate(() => {
        void onListTools({ revision: current.revision }).catch((error) => {
          catalogAcknowledgmentError = error;
        });
      });
    }
    return { tools: current.tools.map(toMCPTool) };
  });
  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const current = catalog;
    const tool = current.byName.get(request.params.name);
    if (tool == null) {
      throw new McpError(
        ErrorCode.InvalidParams,
        `Unknown host tool: ${request.params.name}`
      );
    }
    const input = request.params.arguments ?? {};
    const result = await invoke({
      toolName: tool.name,
      input,
      catalogRevision: current.revision
    });
    return toCallToolResult({ result });
  });
  return {
    server,
    updateCatalog: async ({ revision: nextRevision, tools: nextTools }) => {
      catalog = createCatalog({
        revision: nextRevision,
        tools: nextTools
      });
      await server.sendToolListChanged();
    }
  };
}
function createCatalog({
  revision,
  tools
}) {
  return {
    revision,
    tools: [...tools],
    byName: new Map(tools.map((tool) => [tool.name, tool]))
  };
}
function toMCPTool(tool) {
  return {
    name: tool.name,
    ...tool.description == null ? {} : { description: tool.description },
    inputSchema: requireObjectSchema({
      toolName: tool.name,
      schema: tool.inputSchema ?? { type: "object" }
    })
  };
}
function requireObjectSchema({
  toolName,
  schema
}) {
  if (schema == null || typeof schema !== "object" || Array.isArray(schema) || Reflect.get(schema, "type") !== void 0 && Reflect.get(schema, "type") !== "object") {
    throw new Error(
      `Host tool ${toolName} must use an object JSON Schema for MCP.`
    );
  }
  return schema;
}
function toCallToolResult({
  result
}) {
  return {
    content: [
      {
        type: "text",
        text: stringifyOutput({ output: result.output })
      }
    ],
    ...result.isError ? { isError: true } : {},
    _meta: {
      "ai-sdk-harness-acp-correlation": result.correlationToken
    }
  };
}
function stringifyOutput({ output }) {
  if (output === void 0) return "null";
  try {
    return JSON.stringify(output);
  } catch {
    return JSON.stringify({
      error: "Host tool output could not be serialized as JSON."
    });
  }
}

// src/v1/bridge/host-tool-mcp-http.ts
var HOST_TOOL_MCP_ENDPOINT_PATH = "/mcp";
function createHostToolMCPHttpEndpoint({
  tools,
  revision,
  invoke,
  onListTools
}) {
  let catalog = { revision, tools: [...tools] };
  const sessions = /* @__PURE__ */ new Map();
  let closed = false;
  async function openSession({
    request,
    response,
    body
  }) {
    const hostToolServer = createHostToolMCPServer({
      tools: catalog.tools,
      revision: catalog.revision,
      invoke,
      onListTools
    });
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => randomUUID2(),
      onsessioninitialized: (sessionId) => {
        sessions.set(sessionId, { transport, hostToolServer });
      },
      onsessionclosed: (sessionId) => {
        sessions.delete(sessionId);
      }
    });
    transport.onclose = () => {
      const { sessionId } = transport;
      if (sessionId != null) sessions.delete(sessionId);
    };
    await hostToolServer.server.connect(transport);
    await transport.handleRequest(request, response, body);
  }
  return {
    handleRequest: async ({ request, response, body }) => {
      if (closed) {
        respondWithJSONRPCError({
          response,
          status: 503,
          code: -32e3,
          message: "The host tool MCP endpoint is closed."
        });
        return;
      }
      const sessionId = readSessionId({ request });
      if (sessionId != null) {
        const session2 = sessions.get(sessionId);
        if (session2 == null) {
          respondWithJSONRPCError({
            response,
            status: 404,
            code: -32001,
            message: "Unknown host tool MCP session."
          });
          return;
        }
        await session2.transport.handleRequest(request, response, body);
        return;
      }
      if (request.method !== "POST" || !isInitializeRequest(body)) {
        respondWithJSONRPCError({
          response,
          status: 400,
          code: -32e3,
          message: "Host tool MCP requests without a session id must be an initialize request."
        });
        return;
      }
      await openSession({ request, response, body });
    },
    updateCatalog: async ({ revision: nextRevision, tools: nextTools }) => {
      catalog = { revision: nextRevision, tools: [...nextTools] };
      await Promise.all(
        [...sessions.values()].map(
          (session2) => session2.hostToolServer.updateCatalog({
            revision: nextRevision,
            tools: nextTools
          })
        )
      );
    },
    close: async () => {
      if (closed) return;
      closed = true;
      const live = [...sessions.values()];
      sessions.clear();
      await Promise.all(
        live.map((session2) => session2.hostToolServer.server.close())
      );
    }
  };
}
function readSessionId({
  request
}) {
  const value = request.headers["mcp-session-id"];
  const sessionId = Array.isArray(value) ? value[0] : value;
  return sessionId == null || sessionId.length === 0 ? void 0 : sessionId;
}
function respondWithJSONRPCError({
  response,
  status,
  code,
  message
}) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(
    JSON.stringify({
      jsonrpc: "2.0",
      error: { code, message },
      id: null
    })
  );
}

// src/v1/bridge/host-tool-relay.ts
async function startHostToolRelay({
  tools,
  serverName,
  mcpTransport = "stdio"
}) {
  const state = {
    tools: [...tools],
    fingerprint: catalogFingerprint({ tools }),
    revision: 1,
    servedRevision: 0,
    closed: false,
    changeWaiters: /* @__PURE__ */ new Set(),
    refreshWaiters: /* @__PURE__ */ new Set()
  };
  const credential = randomBytes(32).toString("hex");
  let activeTurn;
  let invocationOrder = 0;
  let closePromise;
  const mcpEndpoint = mcpTransport === "http" ? createHostToolMCPHttpEndpoint({
    tools: state.tools,
    revision: state.revision,
    invoke: ({ toolName, input, catalogRevision }) => handleInvocation({
      body: {
        requestId: randomUUID3(),
        toolName,
        input,
        catalogRevision
      },
      state,
      serverName,
      turn: activeTurn,
      nextInvocationOrder: () => ++invocationOrder
    }),
    onListTools: async ({ revision }) => {
      acknowledgeCatalog({ state, revision });
    }
  }) : void 0;
  const server = createServer(async (request, response) => {
    try {
      if (mcpEndpoint != null && request.url === HOST_TOOL_MCP_ENDPOINT_PATH) {
        if (!credentialsMatch({
          expected: credential,
          actual: request.headers.authorization
        })) {
          throw new RelayRequestError({
            status: 401,
            message: "Invalid host tool relay credential."
          });
        }
        await mcpEndpoint.handleRequest({
          request,
          response,
          ...request.method === "POST" ? { body: await readJSONBody({ request }) } : {}
        });
        return;
      }
      const result = await handleRequest({
        request,
        credential,
        state,
        serverName,
        turn: activeTurn,
        nextInvocationOrder: () => ++invocationOrder
      });
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(result));
    } catch (error) {
      if (response.headersSent) {
        response.end();
        return;
      }
      const status = error instanceof RelayRequestError ? error.status : 500;
      response.writeHead(status, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          error: error instanceof Error ? error.message : String(error)
        })
      );
    }
  });
  await listen({ server });
  const address = server.address();
  return {
    url: `http://127.0.0.1:${address.port}/invoke`,
    ...mcpEndpoint == null ? {} : {
      mcpUrl: `http://127.0.0.1:${address.port}${HOST_TOOL_MCP_ENDPOINT_PATH}`
    },
    credential,
    bindTurn: ({ turn }) => {
      if (activeTurn != null && activeTurn !== turn) {
        throw new Error("A host tool relay turn is already active.");
      }
      activeTurn = turn;
    },
    unbindTurn: ({ turn }) => {
      if (activeTurn === turn) activeTurn = void 0;
    },
    updateCatalog: ({ tools: nextTools }) => {
      const fingerprint = catalogFingerprint({ tools: nextTools });
      if (fingerprint === state.fingerprint) {
        return { changed: false, revision: state.revision };
      }
      state.tools = [...nextTools];
      state.fingerprint = fingerprint;
      state.revision += 1;
      resolveCatalogChanges({ state });
      void mcpEndpoint?.updateCatalog({
        revision: state.revision,
        tools: state.tools
      });
      return { changed: true, revision: state.revision };
    },
    waitForCatalogRefresh: ({ revision, timeoutMs }) => waitForCatalogRefresh({ state, revision, timeoutMs }),
    close: () => {
      if (closePromise != null) return closePromise;
      state.closed = true;
      resolveCatalogChanges({ state });
      resolveRefreshWaiters({ state, closing: true });
      closePromise = (async () => {
        await mcpEndpoint?.close();
        await closeServer({ server });
      })();
      return closePromise;
    }
  };
}
async function handleRequest({
  request,
  credential,
  state,
  serverName,
  turn,
  nextInvocationOrder
}) {
  if (request.method !== "POST") {
    throw new RelayRequestError({
      status: 404,
      message: "Unknown host tool relay endpoint."
    });
  }
  if (!credentialsMatch({
    expected: credential,
    actual: request.headers.authorization
  })) {
    throw new RelayRequestError({
      status: 401,
      message: "Invalid host tool relay credential."
    });
  }
  const body = await readJSONBody({ request });
  if (request.url === "/catalog/next") {
    return handleCatalogNext({ body, state });
  }
  if (request.url === "/catalog/seen") {
    return handleCatalogSeen({ body, state });
  }
  if (request.url === "/invoke") {
    return handleInvocation({
      body,
      state,
      serverName,
      turn,
      nextInvocationOrder
    });
  }
  throw new RelayRequestError({
    status: 404,
    message: "Unknown host tool relay endpoint."
  });
}
async function handleCatalogNext({
  body,
  state
}) {
  if (!isRecord6(body) || !Number.isSafeInteger(body.afterRevision) || body.afterRevision < 0) {
    throw new RelayRequestError({
      status: 400,
      message: "Invalid host tool catalog poll request."
    });
  }
  const afterRevision = body.afterRevision;
  if (!state.closed && afterRevision >= state.revision) {
    await waitForCatalogChange({ state });
  }
  if (state.closed) {
    return { closed: true, revision: state.revision };
  }
  return afterRevision < state.revision ? { revision: state.revision, tools: state.tools } : { revision: state.revision };
}
function handleCatalogSeen({
  body,
  state
}) {
  if (!isRecord6(body) || !Number.isSafeInteger(body.revision) || body.revision < 1 || body.revision > state.revision) {
    throw new RelayRequestError({
      status: 400,
      message: "Invalid host tool catalog acknowledgment."
    });
  }
  acknowledgeCatalog({ state, revision: body.revision });
  return { acknowledged: true };
}
function acknowledgeCatalog({
  state,
  revision
}) {
  state.servedRevision = Math.max(state.servedRevision, revision);
  resolveRefreshWaiters({ state, closing: false });
}
async function handleInvocation({
  body,
  state,
  serverName,
  turn,
  nextInvocationOrder
}) {
  if (turn == null) {
    throw new RelayRequestError({
      status: 409,
      message: "No ACP prompt turn is active."
    });
  }
  if (!isRecord6(body) || typeof body.requestId !== "string" || typeof body.toolName !== "string" || !isRecord6(body.input) || !Number.isSafeInteger(body.catalogRevision)) {
    throw new RelayRequestError({
      status: 400,
      message: "Invalid host tool relay request."
    });
  }
  if (body.catalogRevision !== state.revision) {
    throw new RelayRequestError({
      status: 409,
      message: `Host tool ${body.toolName} was invoked from stale catalog revision ${body.catalogRevision}; the active revision is ${state.revision}.`
    });
  }
  const tool = state.tools.find((item) => item.name === body.toolName);
  if (tool == null) {
    throw new RelayRequestError({
      status: 404,
      message: `Host tool ${body.toolName} is not active in catalog revision ${state.revision}.`
    });
  }
  const correlationToken = randomBytes(32).toString("hex");
  turn.registerCorrelationInvocation({
    token: correlationToken,
    serverName,
    toolName: tool.name,
    input: body.input,
    order: nextInvocationOrder()
  });
  turn.emitToolCall({
    toolCallId: body.requestId,
    toolName: tool.name,
    input: body.input
  });
  let result;
  try {
    result = await turn.requestToolResult(body.requestId);
  } catch (error) {
    turn.removeCorrelationInvocation({ token: correlationToken });
    throw error;
  }
  turn.emitToolResult({
    toolCallId: body.requestId,
    toolName: tool.name,
    output: result.output,
    ...result.isError ? { isError: true } : {}
  });
  return {
    output: result.output,
    ...result.isError ? { isError: true } : {},
    correlationToken
  };
}
function waitForCatalogChange({
  state
}) {
  let changeWaiter;
  const change = new Promise((resolve) => {
    changeWaiter = resolve;
    state.changeWaiters.add(changeWaiter);
  });
  let timer;
  const timeout = new Promise((resolve) => {
    timer = setTimeout(resolve, 2e4);
    timer.unref?.();
  });
  return Promise.race([change, timeout]).finally(() => {
    state.changeWaiters.delete(changeWaiter);
    if (timer != null) clearTimeout(timer);
  });
}
function resolveCatalogChanges({ state }) {
  for (const resolve of [...state.changeWaiters]) resolve();
}
function waitForCatalogRefresh({
  state,
  revision,
  timeoutMs
}) {
  if (state.servedRevision >= revision) return Promise.resolve(true);
  if (state.closed) return Promise.resolve(false);
  let waiter;
  const refresh = new Promise((resolve) => {
    waiter = {
      revision,
      resolve
    };
    state.refreshWaiters.add(waiter);
  });
  let timer;
  const timeout = new Promise((resolve) => {
    timer = setTimeout(() => resolve(false), timeoutMs);
    timer.unref?.();
  });
  return Promise.race([refresh, timeout]).finally(() => {
    state.refreshWaiters.delete(waiter);
    if (timer != null) clearTimeout(timer);
  });
}
function resolveRefreshWaiters({
  state,
  closing
}) {
  for (const waiter of [...state.refreshWaiters]) {
    if (closing || state.servedRevision >= waiter.revision) {
      waiter.resolve(!closing);
    }
  }
}
function catalogFingerprint({
  tools
}) {
  return JSON.stringify(canonicalizeJSON2({ value: tools }));
}
function canonicalizeJSON2({ value }) {
  if (Array.isArray(value)) {
    return value.map((item) => canonicalizeJSON2({ value: item }));
  }
  if (!isRecord6(value)) return value;
  return Object.fromEntries(
    Object.keys(value).sort().filter((key) => value[key] !== void 0).map((key) => [key, canonicalizeJSON2({ value: value[key] })])
  );
}
async function readJSONBody({
  request
}) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += buffer.length;
    if (size > 16 * 1024 * 1024) {
      throw new RelayRequestError({
        status: 413,
        message: "Host tool relay request is too large."
      });
    }
    chunks.push(buffer);
  }
  const text = Buffer.concat(chunks).toString("utf8");
  try {
    return await new Response(text, {
      headers: { "content-type": "application/json" }
    }).json();
  } catch {
    throw new RelayRequestError({
      status: 400,
      message: "Host tool relay request is not valid JSON."
    });
  }
}
function credentialsMatch({
  expected,
  actual
}) {
  const expectedValue = Buffer.from(`Bearer ${expected}`);
  const actualValue = Buffer.from(actual ?? "");
  return expectedValue.length === actualValue.length && timingSafeEqual(expectedValue, actualValue);
}
function listen({ server }) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
}
function closeServer({ server }) {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error == null) resolve();
      else reject(error);
    });
  });
}
function isRecord6(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}
var RelayRequestError = class extends Error {
  constructor({ status, message }) {
    super(message);
    this.name = "HostToolRelayRequestError";
    this.status = status;
  }
};

// src/v1/bridge/host-tool-mcp-definition.ts
import { fileURLToPath } from "url";
import { execPath } from "process";
function createHostToolMcpServerDefinition({
  mcpTransport,
  relay,
  serverName,
  catalogPath,
  initialization,
  harnessId
}) {
  if (mcpTransport === "stdio") {
    return {
      name: serverName,
      command: execPath,
      args: [fileURLToPath(new URL("./host-tool-mcp.mjs", import.meta.url))],
      env: [
        {
          name: "AI_SDK_ACP_HOST_TOOLS_FILE",
          value: catalogPath
        },
        {
          name: "AI_SDK_ACP_HOST_TOOL_RELAY_URL",
          value: relay.url
        },
        {
          name: "AI_SDK_ACP_HOST_TOOL_RELAY_CREDENTIAL",
          value: relay.credential
        }
      ]
    };
  }
  if (initialization.agentCapabilities?.mcpCapabilities?.http !== true) {
    throw new HarnessBridgeCapabilityUnsupportedError({
      harnessId,
      message: "This harness exposes host tools through an HTTP MCP server, but the ACP agent does not advertise support for HTTP MCP servers."
    });
  }
  if (relay.mcpUrl == null) {
    throw new Error("The host tool MCP HTTP endpoint is unavailable.");
  }
  return {
    type: "http",
    name: serverName,
    url: relay.mcpUrl,
    headers: [{ name: "Authorization", value: `Bearer ${relay.credential}` }]
  };
}

// src/v1/bridge/refresh-host-tool-catalog.ts
async function promptAndRefreshInitialHostToolCatalog({
  startPrompt,
  relay,
  tools,
  harnessId,
  timeoutMs
}) {
  void startPrompt();
  await refreshHostToolCatalog({
    relay,
    tools,
    harnessId,
    timeoutMs
  });
}
async function refreshHostToolCatalog({
  relay,
  tools,
  harnessId,
  timeoutMs
}) {
  const catalog = relay.updateCatalog({ tools });
  if (!catalog.changed && tools.length === 0) return;
  if (await relay.waitForCatalogRefresh({
    revision: catalog.revision,
    timeoutMs
  })) {
    return;
  }
  throw new HarnessBridgeCapabilityUnsupportedError({
    harnessId,
    message: `The ACP implementation did not load the active harness-owned MCP tool catalog to revision ${catalog.revision} before the next prompt, so host tools cannot be exposed safely.`
  });
}

// src/v1/bridge/permission-controller.ts
var permissionRequestCounter = 0;
function createACPPermissionController({
  turn,
  sessionId,
  permissionMode,
  hasPermissionModeMapping,
  emitToolCall,
  claimHostToolPermission
}) {
  const pendingPermissions = /* @__PURE__ */ new Map();
  return {
    async requestPermission(request) {
      if (request.sessionId !== sessionId) {
        turn.emitWarning({
          message: `ACP permission request for session ${JSON.stringify(request.sessionId)} was cancelled because the active session is ${JSON.stringify(sessionId)}.`
        });
        return cancelled();
      }
      const allowOnce = request.options.find(
        (option) => option.kind === "allow_once"
      );
      const rejectOnce = request.options.find(
        (option) => option.kind === "reject_once"
      );
      if (allowOnce == null || rejectOnce == null) {
        const missing = [
          ...allowOnce == null ? ["allow_once"] : [],
          ...rejectOnce == null ? ["reject_once"] : []
        ];
        turn.emitWarning({
          message: `ACP permission request for tool call ${JSON.stringify(request.toolCall.toolCallId)} was cancelled because it did not advertise ${missing.join(" and ")}.`
        });
        return cancelled();
      }
      if (claimHostToolPermission({ toolCall: request.toolCall })) {
        return {
          outcome: {
            outcome: "selected",
            optionId: allowOnce.optionId
          }
        };
      }
      if (!hasPermissionModeMapping && shouldAutoApprove({
        permissionMode,
        kind: request.toolCall.kind
      })) {
        return {
          outcome: {
            outcome: "selected",
            optionId: allowOnce.optionId
          }
        };
      }
      const approvalId = `acp-permission-${++permissionRequestCounter}`;
      let cancelPermission;
      const cancellation = new Promise((resolve) => {
        cancelPermission = () => resolve(void 0);
      });
      pendingPermissions.set(approvalId, {
        cancel: cancelPermission
      });
      const approval = turn.requestToolApproval(approvalId);
      emitToolCall({ toolCall: request.toolCall });
      turn.emit({
        type: "tool-approval-request",
        approvalId,
        toolCallId: request.toolCall.toolCallId
      });
      try {
        const response = await Promise.race([approval, cancellation]);
        if (response == null) return cancelled();
        return {
          outcome: {
            outcome: "selected",
            optionId: response.approved ? allowOnce.optionId : rejectOnce.optionId
          }
        };
      } finally {
        pendingPermissions.delete(approvalId);
      }
    },
    cancelAll() {
      for (const pending of pendingPermissions.values()) {
        pending.cancel();
      }
      pendingPermissions.clear();
    }
  };
}
function shouldAutoApprove({
  permissionMode,
  kind
}) {
  if (permissionMode === "allow-all") return true;
  if (kind === "read" || kind === "search" || kind === "think" || kind === "fetch") {
    return true;
  }
  return permissionMode === "allow-edits" && (kind === "edit" || kind === "delete" || kind === "move");
}
function cancelled() {
  return { outcome: { outcome: "cancelled" } };
}

// src/v1/bridge/permission-mode.ts
import * as acp from "@agentclientprotocol/sdk";
var PERMISSION_MODES = [
  "allow-reads",
  "allow-edits",
  "allow-all"
];
async function configureACPPermissionMode({
  agent,
  sessionId,
  sessionConfiguration,
  permissionModeMapping,
  permissionMode,
  harnessId
}) {
  const target = permissionModeMapping[permissionMode];
  if (target == null) {
    throw unsupported({
      harnessId,
      message: `Permission mode ${JSON.stringify(permissionMode)} is not supported by this ACP harness.` + (permissionModeMapping["allow-all"] == null ? "" : ` Use permissionMode: 'allow-all'.`)
    });
  }
  for (const mappedPermissionMode of PERMISSION_MODES) {
    const mappedTarget = permissionModeMapping[mappedPermissionMode];
    if (mappedTarget != null) {
      validateTarget({
        target: mappedTarget,
        permissionMode: mappedPermissionMode,
        sessionConfiguration,
        harnessId
      });
    }
  }
  if (target.type === "session-mode") {
    await agent.request(acp.methods.agent.session.setMode, {
      sessionId,
      modeId: target.modeId
    });
    return;
  }
  await agent.request(acp.methods.agent.session.setConfigOption, {
    sessionId,
    configId: target.configId,
    value: target.value,
    ...typeof target.value === "boolean" ? { type: "boolean" } : {}
  });
}
function validateTarget({
  target,
  permissionMode,
  sessionConfiguration,
  harnessId
}) {
  if (target.type === "session-mode") {
    const availableModeIds = sessionConfiguration.modes?.availableModes.map((mode) => mode.id) ?? [];
    if (!availableModeIds.includes(target.modeId)) {
      throw unsupported({
        harnessId,
        message: `ACP permission mapping for ${JSON.stringify(permissionMode)} requires session mode ${JSON.stringify(target.modeId)}, but the agent advertised ${formatChoices({ values: availableModeIds })}.`
      });
    }
    return;
  }
  const configOption = sessionConfiguration.configOptions?.find(
    (option) => option.id === target.configId
  );
  if (configOption == null) {
    const availableConfigIds = sessionConfiguration.configOptions?.map((option) => option.id) ?? [];
    throw unsupported({
      harnessId,
      message: `ACP permission mapping for ${JSON.stringify(permissionMode)} requires session configuration ${JSON.stringify(target.configId)}, but the agent advertised ${formatChoices({ values: availableConfigIds })}.`
    });
  }
  if (configOption.type === "boolean") {
    if (typeof target.value !== "boolean") {
      throw unsupported({
        harnessId,
        message: `ACP permission mapping for ${JSON.stringify(permissionMode)} assigns string value ${JSON.stringify(target.value)} to boolean session configuration ${JSON.stringify(target.configId)}.`
      });
    }
    return;
  }
  if (typeof target.value !== "string") {
    throw unsupported({
      harnessId,
      message: `ACP permission mapping for ${JSON.stringify(permissionMode)} assigns boolean value ${JSON.stringify(target.value)} to select session configuration ${JSON.stringify(target.configId)}.`
    });
  }
  const availableValues = configOption.options.flatMap(
    (option) => "options" in option ? option.options.map((value) => value.value) : [option.value]
  );
  if (!availableValues.includes(target.value)) {
    throw unsupported({
      harnessId,
      message: `ACP permission mapping for ${JSON.stringify(permissionMode)} requires value ${JSON.stringify(target.value)} for session configuration ${JSON.stringify(target.configId)}, but the agent advertised ${formatChoices({ values: availableValues })}.`
    });
  }
}
function formatChoices({ values }) {
  return values.length === 0 ? "no matching choices" : values.map((value) => JSON.stringify(value)).join(", ");
}
function unsupported({
  harnessId,
  message
}) {
  return new HarnessBridgeCapabilityUnsupportedError({ harnessId, message });
}

// src/v1/bridge/model-mapping.ts
import * as acp2 from "@agentclientprotocol/sdk";
async function configureACPModel({
  agent,
  sessionId,
  model,
  mapping
}) {
  if (model == null) return;
  if (mapping == null) {
    throw new Error("ACP model mapping is required when a model is set.");
  }
  if (mapping.type === "session-model") {
    await agent.request("session/set_model", {
      sessionId,
      [mapping.path]: model
    });
    return;
  }
  await agent.request(acp2.methods.agent.session.setConfigOption, {
    sessionId,
    configId: mapping.path,
    value: model
  });
}

// src/v1/bridge/recovered-session.ts
import * as acp3 from "@agentclientprotocol/sdk";
function assertACPResumeCapability({
  initialization,
  harnessId
}) {
  const sessionCapabilities = initialization.agentCapabilities?.sessionCapabilities;
  if (sessionCapabilities?.resume == null || sessionCapabilities.resume === false) {
    throw new HarnessBridgeCapabilityUnsupportedError({
      harnessId,
      message: "ACP process-loss rerun requires the agent to advertise sessionCapabilities.resume; a fresh unrelated ACP session will not be created."
    });
  }
}
function createACPRecoveredSession({
  agent,
  sessionId,
  restorationResponse,
  updates
}) {
  let disposed = false;
  return {
    sessionId,
    newSessionResponse: {
      sessionId,
      ...restorationResponse
    },
    prompt: async (prompt) => {
      if (disposed) {
        throw new Error("Recovered ACP session is disposed.");
      }
      updates.clearErrors();
      const response = agent.request(
        acp3.methods.agent.session.prompt,
        { sessionId, prompt }
      );
      void response.then(
        (value) => {
          updates.enqueue({
            kind: "stop",
            response: value,
            stopReason: value.stopReason
          });
        },
        (error) => updates.reject(error)
      );
      return response;
    },
    promptWithMeta: async ({ prompt, meta }) => {
      if (disposed) {
        throw new Error("Recovered ACP session is disposed.");
      }
      updates.clearErrors();
      const response = agent.request(
        acp3.methods.agent.session.prompt,
        { sessionId, prompt, _meta: meta }
      );
      void response.then(
        (value) => {
          updates.enqueue({
            kind: "stop",
            response: value,
            stopReason: value.stopReason
          });
        },
        (error) => updates.reject(error)
      );
      return response;
    },
    nextUpdate: () => updates.next(),
    dispose: () => {
      if (disposed) return;
      disposed = true;
      updates.fail(new Error("Recovered ACP session disposed."));
    }
  };
}
function createACPRecoveredSessionUpdates() {
  const values = [];
  const waiters = [];
  let failure;
  let failed = false;
  return {
    enqueue(value) {
      if (failed) return;
      const waiter = waiters.shift();
      if (waiter == null) values.push({ type: "value", value });
      else waiter.resolve(value);
    },
    reject(error) {
      if (failed) return;
      const waiter = waiters.shift();
      if (waiter == null) values.push({ type: "error", error });
      else waiter.reject(error);
    },
    clearErrors() {
      for (let index = values.length - 1; index >= 0; index--) {
        if (values[index]?.type === "error") values.splice(index, 1);
      }
    },
    fail(error) {
      if (failed) return;
      failed = true;
      failure = error;
      for (const waiter of waiters.splice(0)) waiter.reject(error);
    },
    next() {
      const entry = values.shift();
      if (entry?.type === "value") return Promise.resolve(entry.value);
      if (entry?.type === "error") {
        return Promise.reject(
          entry.error instanceof Error ? entry.error : new Error("Recovered ACP session update failed.")
        );
      }
      if (failed) {
        return Promise.reject(
          failure instanceof Error ? failure : new Error("Recovered ACP session update stream failed.")
        );
      }
      return new Promise((resolve, reject) => {
        waiters.push({ resolve, reject });
      });
    }
  };
}

// src/v1/bridge/session-lifecycle.ts
import * as acp4 from "@agentclientprotocol/sdk";
function resolveACPSessionRestorationMethod({
  initialization,
  harnessId
}) {
  const sessionCapabilities = initialization.agentCapabilities?.sessionCapabilities;
  if (sessionCapabilities?.resume != null && sessionCapabilities.resume !== false) {
    return "resume";
  }
  if (initialization.agentCapabilities?.loadSession === true) {
    return "load";
  }
  throw new HarnessBridgeCapabilityUnsupportedError({
    harnessId,
    message: "Cold ACP session restoration requires the agent to advertise sessionCapabilities.resume or loadSession; a fresh unrelated ACP session will not be created."
  });
}
async function restoreACPBridgeSession({
  agent,
  initialization,
  sessionId,
  cwd,
  mcpServers,
  meta,
  harnessId,
  setHistoricalUpdatesSuppressed,
  discardCapturedHistory
}) {
  const method = resolveACPSessionRestorationMethod({
    initialization,
    harnessId
  });
  setHistoricalUpdatesSuppressed({ suppressed: true });
  try {
    const request = {
      sessionId,
      cwd,
      mcpServers: [...mcpServers],
      ...meta == null ? {} : { _meta: meta }
    };
    const response = method === "resume" ? await agent.request(acp4.methods.agent.session.resume, request) : await agent.request(
      acp4.methods.agent.session.load,
      request
    );
    discardCapturedHistory();
    return { method, response };
  } finally {
    setHistoricalUpdatesSuppressed({ suppressed: false });
  }
}

// src/v1/bridge/index.ts
var HOST_TOOL_MCP_SERVER_NAME = "ai-sdk-harness-tools";
var CATALOG_REFRESH_TIMEOUT_MS = 1e4;
var args = parseArgs({ args: argv.slice(2) });
var workDir = requireArg({ value: args.workDir, name: "--workdir" });
var bridgeStateDir = requireArg({
  value: args.bridgeStateDir,
  name: "--bridge-state-dir"
});
var implementationDir = requireArg({
  value: args.implementationDir,
  name: "--implementation-dir"
});
var bridgeType = requireArg({
  value: args.bridgeType,
  name: "--bridge-type"
});
var implementation = await readImplementationDescriptor({
  path: `${implementationDir}/implementation.json`
});
var bridgeConfiguration = await readACPBridgeEnvironment({ env: processEnv });
var child;
var agentResponseStreamFailure;
var connection;
var session;
var recoveredSessionUpdates;
var recoveredSessionId;
var sessionConfigurationFingerprint;
var streamCapture;
var initializationDiagnostic;
var hostToolRelay;
var catalogRefreshError;
var sessionConfigurationFailure;
var historicalUpdatesSuppressed = false;
var coldRestorationMethod;
var activePermissionController;
var activeQuestionRequest;
await runBridge({
  bridgeType,
  bridgeStateDir,
  onStart: runTurn,
  onStop: () => session == null ? {} : { sessionId: session.sessionId },
  onExit: () => {
    let exited = false;
    const finish = () => {
      if (exited) return;
      exited = true;
      session?.dispose();
      connection?.close();
      child?.kill();
      process.exit(0);
    };
    if (hostToolRelay == null) {
      finish();
      return;
    }
    const timer = setTimeout(finish, 1e3);
    timer.unref();
    void hostToolRelay.close().finally(() => {
      clearTimeout(timer);
      finish();
    });
  }
});
async function runTurn(start, turn) {
  let initialHostToolCatalogRefreshRequired;
  try {
    ({ initialHostToolCatalogRefreshRequired } = await ensureSession({
      start,
      turn
    }));
  } catch (error) {
    if (HarnessBridgeCapabilityUnsupportedError.isInstance(error)) throw error;
    throw createACPBridgeError({
      stage: "session initialization",
      cause: error
    });
  }
  const activeSession = session;
  if (activeSession == null) {
    throw new Error("ACP session initialization did not produce a session.");
  }
  const activeAgentResponseStreamFailure = agentResponseStreamFailure;
  if (activeAgentResponseStreamFailure == null) {
    throw new Error(
      "ACP session initialization did not start stderr monitoring."
    );
  }
  const activeHostToolRelay = hostToolRelay;
  if (activeHostToolRelay == null) {
    throw new Error("The host tool MCP relay is unavailable.");
  }
  if (start.recoveryMode?.type === "lossy-rerun") {
    const marker = {
      type: "acp-recovery",
      mode: "lossy-rerun",
      reason: start.recoveryMode.reason
    };
    turn.emit({ type: "raw", rawValue: marker });
    turn.bridgeLog({
      level: "warn",
      subsystem: "acp.recovery",
      message: "The ACP process was replaced; the original prompt is being rerun against the resumed ACP session.",
      attrs: marker
    });
    turn.emitWarning({
      message: "ACP process-loss recovery is rerunning the interrupted prompt and may repeat work."
    });
  }
  turn.emit({
    type: "bridge-thread",
    threadId: activeSession.sessionId
  });
  if (initializationDiagnostic != null) {
    turn.bridgeLog({
      level: "info",
      subsystem: "acp.protocol",
      message: "ACP session initialized.",
      attrs: initializationDiagnostic
    });
  }
  if (start.recoveryMode?.type === "cold-restore") {
    const marker = {
      type: "acp-session-restored",
      method: coldRestorationMethod
    };
    turn.emit({ type: "raw", rawValue: marker });
    turn.bridgeLog({
      level: "info",
      subsystem: "acp.lifecycle",
      message: "ACP session restored in a replacement bridge process.",
      attrs: marker
    });
    turn.emit({
      type: "finish",
      finishReason: {
        unified: "stop",
        raw: "acp-session-restored"
      },
      totalUsage: unknownUsage2()
    });
    return;
  }
  await configureACPModel({
    agent: connection.agent,
    sessionId: activeSession.sessionId,
    model: start.model,
    mapping: start.modelMapping
  });
  let rejectCancellationFailure;
  const cancellationFailure = new Promise((_, reject) => {
    rejectCancellationFailure = reject;
  });
  void cancellationFailure.catch(() => {
  });
  let cancellationRequested = false;
  let cancellationFailureError;
  const cancel = async () => {
    if (cancellationRequested) return;
    cancellationRequested = true;
    activePermissionController?.cancelAll();
    try {
      if (connection == null) {
        throw new Error("ACP connection closed before cancellation.");
      }
      await connection.agent.notify(acp5.methods.agent.session.cancel, {
        sessionId: activeSession.sessionId
      });
    } catch (error) {
      cancellationFailureError = createACPBridgeError({
        stage: "session cancellation",
        cause: error
      });
      rejectCancellationFailure(cancellationFailureError);
    }
  };
  turn.emit({ type: "stream-start" });
  const emitStreamEvent = createEmitStreamEvent({
    emit: (event) => turn.emit(event),
    emitToolCallCandidate: ({ toolCall }) => {
      const requestId = crypto.randomUUID();
      turn.emit({
        type: "acp-tool-call-candidate",
        requestId,
        toolCall
      });
      void turn.requestToolResult(requestId).then((result) => {
        if (result.output != null && typeof result.output === "object" && "suppress" in result.output && result.output.suppress === true) {
          emitStreamEvent.suppressToolCall({
            toolCallId: toolCall.toolCallId
          });
        }
      });
    },
    builtinTools: start.builtinTools,
    hostToolServerName: HOST_TOOL_MCP_SERVER_NAME,
    hostTools: start.tools ?? []
  });
  if (bridgeConfiguration.askUserQuestionsRequestMethod != null) {
    activeQuestionRequest = {
      method: bridgeConfiguration.askUserQuestionsRequestMethod,
      turn,
      emitStreamEvent
    };
  }
  const permissionController = createACPPermissionController({
    turn,
    sessionId: activeSession.sessionId,
    permissionMode: start.permissionMode ?? "allow-all",
    hasPermissionModeMapping: start.permissionModeMapping != null,
    emitToolCall: emitStreamEvent.permissionToolCall,
    claimHostToolPermission: emitStreamEvent.claimHostToolPermission
  });
  activePermissionController = permissionController;
  const relayTurn = {
    emitToolCall: emitStreamEvent.hostToolCall,
    emitToolResult: emitStreamEvent.hostToolResult,
    requestToolResult: (toolCallId) => turn.requestToolResult(toolCallId),
    registerCorrelationInvocation: emitStreamEvent.registerHostToolCorrelationInvocation,
    removeCorrelationInvocation: emitStreamEvent.removeHostToolCorrelationInvocation
  };
  activeHostToolRelay.bindTurn({ turn: relayTurn });
  try {
    const promptMeta = createOutputSchemaPromptMeta({ start });
    const startPrompt = () => promptActiveSession({
      session: activeSession,
      agent: connection.agent,
      prompt: start.prompt,
      meta: promptMeta
    });
    if (initialHostToolCatalogRefreshRequired) {
      try {
        await promptAndRefreshInitialHostToolCatalog({
          startPrompt,
          relay: activeHostToolRelay,
          tools: start.tools ?? [],
          harnessId: bridgeType,
          timeoutMs: CATALOG_REFRESH_TIMEOUT_MS
        });
      } catch (error) {
        if (HarnessBridgeCapabilityUnsupportedError.isInstance(error)) {
          catalogRefreshError = error;
        }
        sessionConfigurationFailure = { error };
        throw error;
      }
    } else {
      void startPrompt();
    }
    if (turn.abortSignal.aborted) {
      await cancel();
    } else {
      turn.abortSignal.addEventListener("abort", () => void cancel(), {
        once: true
      });
    }
    for (; ; ) {
      let message;
      try {
        message = await Promise.race([
          activeSession.nextUpdate(),
          cancellationFailure,
          activeAgentResponseStreamFailure
        ]);
      } catch (error) {
        for (const rawValue of streamCapture?.drainRawValues() ?? []) {
          emitStreamEvent.raw({ rawValue });
        }
        emitStreamEvent.close();
        if (error === cancellationFailureError) throw error;
        throw createACPBridgeError({
          stage: "prompt update stream",
          cause: error
        });
      }
      if (message.kind === "session_update") {
        const captured = streamCapture?.takeForUpdate({
          update: message.update
        });
        for (const rawValue of captured?.precedingRawValues ?? []) {
          emitStreamEvent.raw({ rawValue });
        }
        emitStreamEvent.message({
          message,
          rawUpdate: captured?.rawUpdate
        });
        continue;
      }
      for (const rawValue of streamCapture?.drainRawValues() ?? []) {
        emitStreamEvent.raw({ rawValue });
      }
      if (emitStreamEvent.message({ message })) return;
    }
  } finally {
    if (activeQuestionRequest?.turn === turn) {
      activeQuestionRequest = void 0;
    }
    permissionController.cancelAll();
    activePermissionController = void 0;
    activeHostToolRelay.unbindTurn({ turn: relayTurn });
  }
}
async function ensureSession({
  start,
  turn
}) {
  if (sessionConfigurationFailure != null) {
    throw sessionConfigurationFailure.error;
  }
  const fingerprint = JSON.stringify({
    authentication: bridgeConfiguration.authentication,
    providerAuthentication: bridgeConfiguration.providerAuthentication,
    providerEnvironment: bridgeConfiguration.providerEnvironment,
    sessionMeta: bridgeConfiguration.sessionMeta,
    instructionMapping: start.instructionMapping,
    permissionMode: start.permissionMode,
    permissionModeMapping: start.permissionModeMapping,
    mcpServers: start.mcpServers
  });
  if (session != null) {
    if (catalogRefreshError != null) throw catalogRefreshError;
    if (sessionConfigurationFingerprint !== fingerprint) {
      throw new Error(
        "ACP authentication and session profile settings cannot change after the ACP session has started."
      );
    }
    const relay = hostToolRelay;
    if (relay == null) {
      throw new Error("The host tool MCP relay is unavailable.");
    }
    try {
      await refreshHostToolCatalog({
        relay,
        tools: start.tools ?? [],
        harnessId: bridgeType,
        timeoutMs: CATALOG_REFRESH_TIMEOUT_MS
      });
    } catch (error) {
      if (HarnessBridgeCapabilityUnsupportedError.isInstance(error)) {
        catalogRefreshError = error;
      }
      throw error;
    }
    return { initialHostToolCatalogRefreshRequired: false };
  }
  const clientApp = resolveClientApp();
  const authentication = bridgeConfiguration.authentication;
  const launchEnv = bridgeConfiguration.providerEnvironment ?? resolveACPLaunchEnvironment({
    providerAuthentication: bridgeConfiguration.providerAuthentication,
    gateway: bridgeConfiguration.providerAuthentication?.type === "ai-gateway" ? resolveGatewayValues({ clientApp }) : void 0
  });
  const instructionConfiguration = await resolveACPInstructionConfiguration({
    instructions: start.instructions,
    instructionMapping: start.instructionMapping,
    sessionMeta: bridgeConfiguration.sessionMeta,
    environment: {
      ...createChildEnvironment({
        launchEnv,
        implementationDir,
        privateHome: implementation.privateHome
      })
    }
  });
  child = spawn(
    `${implementationDir}/${implementation.executablePath}`,
    [...implementation.args],
    {
      cwd: workDir,
      env: instructionConfiguration.environment,
      shell: false,
      stdio: ["pipe", "pipe", "pipe"]
    }
  );
  agentResponseStreamFailure = monitorACPAgentStderr({
    stderr: child.stderr,
    onStderrLine: (line) => {
      turn.bridgeLog({
        level: "warn",
        subsystem: "acp.agent.stderr",
        message: line
      });
    }
  });
  const input = Writable.toWeb(child.stdin);
  const output = Readable.toWeb(child.stdout);
  const capturedStream = captureACPStream({
    stream: acp5.ndJsonStream(input, output)
  });
  streamCapture = capturedStream.capture;
  let client2 = acp5.client({ name: clientApp.name }).onRequest(
    acp5.methods.client.session.requestPermission,
    ({ params }) => activePermissionController?.requestPermission(params) ?? {
      outcome: { outcome: "cancelled" }
    }
  ).onNotification(acp5.methods.client.session.update, ({ params }) => {
    if (params.sessionId !== recoveredSessionId) return;
    if (historicalUpdatesSuppressed) return;
    recoveredSessionUpdates?.enqueue({
      kind: "session_update",
      notification: params,
      update: params.update
    });
  });
  if (bridgeConfiguration.askUserQuestionsRequestMethod != null) {
    const method = bridgeConfiguration.askUserQuestionsRequestMethod;
    client2 = client2.onRequest(
      method,
      (value) => value,
      async ({ params }) => {
        const active = activeQuestionRequest;
        if (active == null || active.method !== method) {
          throw acp5.RequestError.methodNotFound(method);
        }
        const requestId = crypto.randomUUID();
        const nativeToolCallId = getNativeQuestionToolCallId({ params });
        active.turn.emit({
          type: "acp-question-request",
          requestId,
          nativeRequest: params,
          ...nativeToolCallId == null ? {} : {
            nativeToolCall: active.emitStreamEvent.getToolCall({
              toolCallId: nativeToolCallId
            }) ?? void 0
          }
        });
        const result = await active.turn.requestToolResult(requestId);
        const classification = result.output;
        if (classification?.type !== "handled") {
          throw acp5.RequestError.methodNotFound(method);
        }
        active.emitStreamEvent.suppressToolCall({
          toolCallId: nativeToolCallId ?? classification.toolCallId
        });
        try {
          const nativeResult = await active.turn.requestToolResult(
            classification.toolCallId
          );
          return nativeResult.output;
        } finally {
          active.turn.emit({
            type: "acp-question-resolved",
            requestId
          });
        }
      }
    );
  }
  connection = client2.connect(capturedStream.stream);
  const initializeRequest = createACPInitializeRequest({
    protocolVersion: acp5.PROTOCOL_VERSION,
    clientApp,
    authentication,
    clientCapabilities: bridgeConfiguration.clientCapabilities,
    supportsBooleanSessionConfigOptions: Object.values(
      start.permissionModeMapping ?? {}
    ).some(
      (target) => target?.type === "session-config-option" && typeof target.value === "boolean"
    )
  });
  const initialization = await connection.agent.request(acp5.methods.agent.initialize, initializeRequest);
  validateACPProtocolVersion({
    requested: acp5.PROTOCOL_VERSION,
    initialization
  });
  if (authentication != null) {
    await authenticate({
      agent: connection.agent,
      initialization,
      authentication
    });
  }
  const externalMcpServers = createExternalMcpServers({
    mcpServers: start.mcpServers,
    initialization
  });
  const tools = start.tools ?? [];
  const mcpTransport = bridgeConfiguration.hostToolMcpTransport ?? "stdio";
  const catalogPath = `${bridgeStateDir}/host-tools.json`;
  if (mcpTransport === "stdio") {
    await writeFile2(catalogPath, JSON.stringify(tools), { mode: 384 });
  }
  hostToolRelay = await startHostToolRelay({
    tools,
    serverName: HOST_TOOL_MCP_SERVER_NAME,
    mcpTransport
  });
  const mcpServers = [
    ...externalMcpServers,
    createHostToolMcpServerDefinition({
      mcpTransport,
      relay: hostToolRelay,
      serverName: HOST_TOOL_MCP_SERVER_NAME,
      catalogPath,
      initialization,
      harnessId: bridgeType
    })
  ];
  let createdSession;
  if (start.recoveryMode?.type === "lossy-rerun") {
    assertACPResumeCapability({ initialization, harnessId: bridgeType });
    recoveredSessionId = start.recoveryMode.acpSessionId;
    recoveredSessionUpdates = createACPRecoveredSessionUpdates();
    const resumeResponse = await connection.agent.request(acp5.methods.agent.session.resume, {
      sessionId: recoveredSessionId,
      cwd: workDir,
      mcpServers,
      ...instructionConfiguration.sessionMeta == null ? {} : { _meta: instructionConfiguration.sessionMeta }
    });
    createdSession = createACPRecoveredSession({
      agent: connection.agent,
      sessionId: recoveredSessionId,
      restorationResponse: resumeResponse,
      updates: recoveredSessionUpdates
    });
  } else if (start.recoveryMode?.type === "cold-restore") {
    recoveredSessionId = start.recoveryMode.acpSessionId;
    recoveredSessionUpdates = createACPRecoveredSessionUpdates();
    const restored = await restoreACPBridgeSession({
      agent: connection.agent,
      initialization,
      sessionId: recoveredSessionId,
      cwd: workDir,
      mcpServers,
      meta: instructionConfiguration.sessionMeta,
      harnessId: bridgeType,
      setHistoricalUpdatesSuppressed: ({ suppressed }) => {
        historicalUpdatesSuppressed = suppressed;
      },
      discardCapturedHistory: () => {
        streamCapture?.drainRawValues();
      }
    });
    coldRestorationMethod = restored.method;
    createdSession = createACPRecoveredSession({
      agent: connection.agent,
      sessionId: recoveredSessionId,
      restorationResponse: restored.response,
      updates: recoveredSessionUpdates
    });
  } else {
    createdSession = await connection.agent.buildSession({
      cwd: workDir,
      mcpServers,
      ...instructionConfiguration.sessionMeta == null ? {} : { _meta: instructionConfiguration.sessionMeta }
    }).start();
  }
  try {
    if (start.permissionModeMapping != null) {
      await configureACPPermissionMode({
        agent: connection.agent,
        sessionId: createdSession.sessionId,
        sessionConfiguration: createdSession.newSessionResponse,
        permissionModeMapping: start.permissionModeMapping,
        permissionMode: start.permissionMode ?? "allow-all",
        harnessId: bridgeType
      });
    }
  } catch (error) {
    createdSession.dispose();
    if (HarnessBridgeCapabilityUnsupportedError.isInstance(error)) {
      catalogRefreshError = error;
    }
    sessionConfigurationFailure = { error };
    throw error;
  }
  session = createdSession;
  initializationDiagnostic = createACPInitializationDiagnostic({
    initialization,
    sessionId: createdSession.sessionId
  });
  sessionConfigurationFingerprint = fingerprint;
  return { initialHostToolCatalogRefreshRequired: tools.length > 0 };
}
function getNativeQuestionToolCallId({
  params
}) {
  if (!isRecord7(params)) return void 0;
  if (typeof params.toolCallId === "string") return params.toolCallId;
  const nestedParams = params.params;
  return isRecord7(nestedParams) && typeof nestedParams.toolCallId === "string" ? nestedParams.toolCallId : void 0;
}
function createExternalMcpServers({
  mcpServers,
  initialization
}) {
  if (mcpServers == null) return [];
  return Object.entries(mcpServers).map(([name2, value]) => {
    if (!isRecord7(value)) {
      throw new Error(
        `ACP MCP server ${JSON.stringify(name2)} must be configured with an object value.`
      );
    }
    if (value.type === "acp") {
      throw new HarnessBridgeCapabilityUnsupportedError({
        harnessId: bridgeType,
        message: "ACP-transport MCP servers require client-side mcp/connect handling, which this harness does not provide."
      });
    }
    const mcpCapabilities = initialization.agentCapabilities?.mcpCapabilities;
    if (value.type === "http" && mcpCapabilities?.http !== true || value.type === "sse" && mcpCapabilities?.sse !== true) {
      throw new HarnessBridgeCapabilityUnsupportedError({
        harnessId: bridgeType,
        message: `The ACP agent does not advertise support for ${value.type.toUpperCase()} MCP servers.`
      });
    }
    return { ...value, name: name2 };
  });
}
function unknownUsage2() {
  return {
    inputTokens: {
      total: void 0,
      noCache: void 0,
      cacheRead: void 0,
      cacheWrite: void 0
    },
    outputTokens: {
      total: void 0,
      text: void 0,
      reasoning: void 0
    }
  };
}
async function authenticate({
  agent,
  initialization,
  authentication
}) {
  assertACPAuthenticationMethod({
    initialization,
    methodId: authentication.methodId
  });
  await agent.request(acp5.methods.agent.authenticate, {
    methodId: authentication.methodId,
    ...authentication.meta == null ? {} : { _meta: authentication.meta }
  });
}
function resolveClientApp() {
  const name2 = processEnv.AI_SDK_ACP_CLIENT_APP_NAME;
  const version = processEnv.AI_SDK_ACP_CLIENT_APP_VERSION;
  if (name2 == null || version == null) {
    throw new Error("ACP client app values were not supplied to the bridge.");
  }
  return { name: name2, version };
}
function resolveGatewayValues({
  clientApp
}) {
  const apiKey = processEnv.AI_SDK_ACP_GATEWAY_API_KEY;
  const baseUrl = processEnv.AI_SDK_ACP_GATEWAY_BASE_URL;
  if (apiKey == null || baseUrl == null) {
    throw new Error(
      "AI Gateway profile values were not supplied to the ACP bridge."
    );
  }
  return {
    apiKey,
    baseUrl,
    clientAppName: clientApp.name,
    clientAppVersion: clientApp.version
  };
}
function createChildEnvironment({
  launchEnv,
  implementationDir: implementationDir2,
  privateHome
}) {
  const blocked = /* @__PURE__ */ new Set([
    "BRIDGE_CHANNEL_TOKEN",
    "BRIDGE_WS_PORT",
    "AI_SDK_ACP_GATEWAY_API_KEY",
    "AI_SDK_ACP_GATEWAY_BASE_URL",
    "AI_SDK_ACP_CLIENT_APP_NAME",
    "AI_SDK_ACP_CLIENT_APP_VERSION",
    ACP_BRIDGE_CONFIGURATION_ENV
  ]);
  const environment = {
    ...Object.fromEntries(
      Object.entries(processEnv).filter(
        ([key, value]) => !blocked.has(key) && value != null
      )
    ),
    ...launchEnv
  };
  if (!privateHome) return environment;
  const home = `${implementationDir2}/home`;
  const inheritedPath = environment.PATH;
  return {
    ...environment,
    HOME: home,
    PATH: [
      `${home}/.local/bin`,
      ...inheritedPath == null || inheritedPath.length === 0 ? [] : [inheritedPath]
    ].join(":")
  };
}
function isRecord7(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}
function createOutputSchemaPromptMeta({
  start
}) {
  if (start.responseFormat?.type !== "json" || start.responseFormat.schema == null || start.outputSchemaMapping?.type !== "session-prompt-meta") {
    return void 0;
  }
  const root = {};
  let target = root;
  const path = start.outputSchemaMapping.path;
  for (let index = 0; index < path.length - 1; index++) {
    const child2 = {};
    target[path[index]] = child2;
    target = child2;
  }
  target[path[path.length - 1]] = start.responseFormat.schema;
  return root;
}
function promptActiveSession({
  session: session2,
  agent,
  prompt,
  meta
}) {
  if (meta == null) return session2.prompt(prompt);
  if (session2.promptWithMeta != null) {
    return session2.promptWithMeta({ prompt, meta });
  }
  const updates = session2.updates;
  if (updates == null) {
    throw new Error(
      "The installed ACP SDK cannot send session prompt metadata while preserving streamed updates."
    );
  }
  updates.clearErrors();
  const response = agent.request(
    acp5.methods.agent.session.prompt,
    {
      sessionId: session2.sessionId,
      prompt,
      _meta: meta
    }
  );
  void response.then(
    (value) => {
      updates.enqueue({
        kind: "stop",
        response: value,
        stopReason: value.stopReason
      });
    },
    (error) => updates.reject(error)
  );
  return response;
}
async function readImplementationDescriptor({
  path
}) {
  const text = await readFile(path, "utf8");
  const value = await new Response(text, {
    headers: { "content-type": "application/json" }
  }).json();
  if (!isRecord7(value) || typeof value.executablePath !== "string" || typeof value.privateHome !== "boolean" || !Array.isArray(value.args) || !value.args.every((item) => typeof item === "string") || !Array.isArray(value.envKeys) || !value.envKeys.every((item) => typeof item === "string")) {
    throw new Error("Invalid ACP implementation descriptor.");
  }
  return {
    executablePath: value.executablePath,
    privateHome: value.privateHome,
    args: value.args,
    envKeys: value.envKeys
  };
}
function parseArgs({ args: args2 }) {
  const result = {};
  for (let index = 0; index < args2.length; index++) {
    const value = args2[index + 1];
    if (value == null) continue;
    if (args2[index] === "--workdir") result.workDir = value;
    else if (args2[index] === "--bridge-state-dir")
      result.bridgeStateDir = value;
    else if (args2[index] === "--implementation-dir")
      result.implementationDir = value;
    else if (args2[index] === "--bridge-type") result.bridgeType = value;
  }
  return result;
}
function requireArg({
  value,
  name: name2
}) {
  if (value == null || value.length === 0) {
    throw new Error(`Missing ${name2} argument.`);
  }
  return value;
}
//# sourceMappingURL=index.mjs.map