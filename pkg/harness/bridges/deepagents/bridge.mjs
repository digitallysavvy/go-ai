// src/bridge/index.ts
import { randomUUID as randomUUID3 } from "crypto";
import { argv, env as procEnv2 } from "process";

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
import { ChatAnthropic } from "@langchain/anthropic";
import { tool as tool3 } from "@langchain/core/tools";
import { Command as Command5, MemorySaver as MemorySaver2, Overwrite } from "@langchain/langgraph";
import {
  MultiServerMCPClient
} from "@langchain/mcp-adapters";
import { createDeepAgent } from "deepagents";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/chat_models/universal.js
import { BaseChatModel } from "@langchain/core/language_models/chat_models";
import { RunnableBinding, ensureConfig } from "@langchain/core/runnables";
import { AsyncGeneratorWithSetup, IterableReadableStream } from "@langchain/core/utils/stream";
import { ChatModelStream } from "@langchain/core/language_models/stream";
import { DEFAULT_LANGSMITH_GATEWAY, resolveLangSmithGatewayConfig } from "@langchain/core/utils/gateway";
import { getEnvironmentVariable } from "@langchain/core/utils/env";
var MODEL_PROVIDER_CONFIG = {
  openai: {
    package: "@langchain/openai",
    className: "ChatOpenAI"
  },
  anthropic: {
    package: "@langchain/anthropic",
    className: "ChatAnthropic"
  },
  azure_openai: {
    package: "@langchain/openai",
    className: "AzureChatOpenAI"
  },
  langsmith: {
    package: "@langchain/openai",
    className: "ChatOpenAI"
  },
  cohere: {
    package: "@langchain/cohere",
    className: "ChatCohere"
  },
  google: {
    package: "@langchain/google",
    className: "ChatGoogle"
  },
  "google-vertexai": {
    package: "@langchain/google-vertexai",
    className: "ChatVertexAI"
  },
  "google-vertexai-web": {
    package: "@langchain/google-vertexai-web",
    className: "ChatVertexAI"
  },
  "google-genai": {
    package: "@langchain/google-genai",
    className: "ChatGoogleGenerativeAI"
  },
  ollama: {
    package: "@langchain/ollama",
    className: "ChatOllama"
  },
  mistralai: {
    package: "@langchain/mistralai",
    className: "ChatMistralAI"
  },
  mistral: {
    package: "@langchain/mistralai",
    className: "ChatMistralAI"
  },
  groq: {
    package: "@langchain/groq",
    className: "ChatGroq"
  },
  bedrock: {
    package: "@langchain/aws",
    className: "ChatBedrockConverse"
  },
  aws: {
    package: "@langchain/aws",
    className: "ChatBedrockConverse"
  },
  deepseek: {
    package: "@langchain/deepseek",
    className: "ChatDeepSeek"
  },
  xai: {
    package: "@langchain/xai",
    className: "ChatXAI"
  },
  cerebras: {
    package: "@langchain/cerebras",
    className: "ChatCerebras"
  },
  fireworks: {
    package: "@langchain/fireworks",
    className: "ChatFireworks"
  },
  together: {
    package: "@langchain/together-ai",
    className: "ChatTogetherAI",
    hasCircularDependency: true
  },
  perplexity: {
    package: "@langchain/perplexity",
    className: "ChatPerplexity"
  }
};
var SUPPORTED_PROVIDERS = Object.keys(MODEL_PROVIDER_CONFIG);

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/tools/headless.js
import { tool } from "@langchain/core/tools";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/errors.js
import { isGraphBubbleUp } from "@langchain/langgraph";
var StructuredOutputParsingError = class extends Error {
  toolName;
  errors;
  constructor(toolName, errors) {
    super(`Failed to parse structured output for tool '${toolName}':${errors.map((e) => `
  - ${e}`).join("")}.`);
    this.toolName = toolName;
    this.errors = errors;
  }
};

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/model.js
function isBaseChatModel(model) {
  return "invoke" in model && typeof model.invoke === "function" && "_streamResponseChunks" in model;
}

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/responses.js
import { isInteropZodObject, isInteropZodSchema } from "@langchain/core/utils/types";
import { Validator, toJsonSchema } from "@langchain/core/utils/json_schema";
import { isSerializableSchema } from "@langchain/core/utils/standard_schema";
var PROVIDER_STRATEGY_DEFAULT_STRICT = true;
var bindingIdentifier = 0;
var ToolStrategy = class ToolStrategy2 {
  schema;
  tool;
  options;
  constructor(schema, tool4, options) {
    this.schema = schema;
    this.tool = tool4;
    this.options = options;
  }
  get name() {
    return this.tool.function.name;
  }
  static fromSchema(schema, outputOptions) {
    function getFunctionName(name) {
      return name ?? `extract-${++bindingIdentifier}`;
    }
    if (isSerializableSchema(schema) || isInteropZodSchema(schema)) {
      const asJsonSchema2 = toJsonSchema(schema);
      const tool4 = {
        type: "function",
        function: {
          name: getFunctionName(asJsonSchema2.title),
          strict: false,
          description: asJsonSchema2.description ?? "Tool for extracting structured output from the model's response.",
          parameters: asJsonSchema2
        }
      };
      return new ToolStrategy2(asJsonSchema2, tool4, outputOptions);
    }
    let functionDefinition;
    if (typeof schema.name === "string" && typeof schema.parameters === "object" && schema.parameters != null) functionDefinition = schema;
    else functionDefinition = {
      name: getFunctionName(schema.title),
      description: schema.description ?? "",
      parameters: schema.schema || schema
    };
    const asJsonSchema = toJsonSchema(schema);
    return new ToolStrategy2(asJsonSchema, {
      type: "function",
      function: functionDefinition
    }, outputOptions);
  }
  /**
  * Parse tool arguments according to the schema.
  *
  * @throws {StructuredOutputParsingError} if the response is not valid
  * @param toolArgs - The arguments from the tool call
  * @returns The parsed response according to the schema type
  */
  parse(toolArgs) {
    const result = new Validator(this.schema).validate(toolArgs);
    if (!result.valid) throw new StructuredOutputParsingError(this.name, result.errors.map((e) => e.error));
    return toolArgs;
  }
};
var ProviderStrategy = class ProviderStrategy2 {
  _schemaType;
  /**
  * The schema to use for the provider strategy
  */
  schema;
  /**
  * Whether to use strict mode for the provider strategy
  */
  strict;
  constructor(schemaOrOptions, strict) {
    if ("schema" in schemaOrOptions && typeof schemaOrOptions.schema === "object" && schemaOrOptions.schema !== null && !("type" in schemaOrOptions)) {
      const options = schemaOrOptions;
      this.schema = options.schema;
      this.strict = options.strict ?? PROVIDER_STRATEGY_DEFAULT_STRICT;
    } else {
      this.schema = schemaOrOptions;
      this.strict = strict ?? PROVIDER_STRATEGY_DEFAULT_STRICT;
    }
  }
  static fromSchema(schema, strict) {
    const asJsonSchema = toJsonSchema(schema);
    return new ProviderStrategy2(asJsonSchema, strict);
  }
  /**
  * Parse tool arguments according to the schema. If the response is not valid, return undefined.
  *
  * @param response - The AI message response to parse
  * @returns The parsed response according to the schema type
  */
  parse(response) {
    let textContent;
    if (typeof response.content === "string") textContent = response.content;
    else if (Array.isArray(response.content)) {
      for (const block of response.content) if (typeof block === "object" && block !== null && "type" in block && block.type === "text" && !("thought" in block && block.thought === true) && "text" in block && typeof block.text === "string") {
        textContent = block.text;
        break;
      }
    }
    if (!textContent || textContent === "") return;
    try {
      const content = JSON.parse(textContent);
      if (!new Validator(this.schema).validate(content).valid) return;
      return content;
    } catch {
    }
  }
};
function transformResponseFormat(responseFormat, options, model) {
  if (!responseFormat) return [];
  if (typeof responseFormat === "object" && responseFormat !== null && "__responseFormatUndefined" in responseFormat) return [];
  if (Array.isArray(responseFormat)) {
    if (responseFormat.every((item) => item instanceof ToolStrategy || item instanceof ProviderStrategy)) return responseFormat;
    if (responseFormat.every((item) => isSerializableSchema(item))) return responseFormat.map((item) => ToolStrategy.fromSchema(item, options));
    if (responseFormat.every((item) => isInteropZodObject(item))) return responseFormat.map((item) => ToolStrategy.fromSchema(item, options));
    if (responseFormat.every((item) => typeof item === "object" && item !== null && !isInteropZodObject(item) && !isSerializableSchema(item))) return responseFormat.map((item) => ToolStrategy.fromSchema(item, options));
    throw new Error("Invalid response format: list contains mixed types.\nAll items must be either InteropZodObject, Standard Schema, or plain JSON schema objects.");
  }
  if (responseFormat instanceof ToolStrategy || responseFormat instanceof ProviderStrategy) return [responseFormat];
  const useProviderStrategy = hasSupportForJsonSchemaOutput(model);
  if (isSerializableSchema(responseFormat)) return useProviderStrategy ? [ProviderStrategy.fromSchema(responseFormat)] : [ToolStrategy.fromSchema(responseFormat, options)];
  if (isInteropZodObject(responseFormat)) return useProviderStrategy ? [ProviderStrategy.fromSchema(responseFormat)] : [ToolStrategy.fromSchema(responseFormat, options)];
  if (typeof responseFormat === "object" && responseFormat !== null && "properties" in responseFormat) return useProviderStrategy ? [ProviderStrategy.fromSchema(responseFormat)] : [ToolStrategy.fromSchema(responseFormat, options)];
  throw new Error(`Invalid response format: ${String(responseFormat)}`);
}
function toolStrategy(responseFormat, options) {
  return transformResponseFormat(responseFormat, options);
}
function hasSupportForJsonSchemaOutput(model) {
  if (!model || !isBaseChatModel(model) || !("profile" in model) || typeof model.profile !== "object" || !model.profile) return false;
  return "structuredOutput" in model.profile && model.profile.structuredOutput === true;
}

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/utils.js
import { AIMessage, ToolMessage } from "@langchain/core/messages";
import { isLangChainTool } from "@langchain/core/tools";
import { convertToOpenAITool } from "@langchain/core/utils/function_calling";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/transformers/tool-call.js
import { ToolMessage as ToolMessage2 } from "@langchain/core/messages";
import { StreamChannel } from "@langchain/langgraph";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/transformers/subagent.js
import { StreamChannel as StreamChannel2, createMessagesTransformer } from "@langchain/langgraph";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/types.js
var MIDDLEWARE_BRAND = /* @__PURE__ */ Symbol.for("AgentMiddleware");

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware.js
function createMiddleware(config) {
  return {
    [MIDDLEWARE_BRAND]: true,
    name: config.name,
    stateSchema: config.stateSchema,
    contextSchema: config.contextSchema,
    wrapToolCall: config.wrapToolCall,
    wrapModelCall: config.wrapModelCall,
    beforeAgent: config.beforeAgent,
    beforeModel: config.beforeModel,
    afterModel: config.afterModel,
    afterAgent: config.afterAgent,
    tools: config.tools,
    streamTransformers: config.streamTransformers
  };
}

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/tests/utils.js
import { AIMessage as AIMessage2, HumanMessage } from "@langchain/core/messages";
import { BaseChatModel as BaseChatModel2 } from "@langchain/core/language_models/chat_models";
import { RunnableLambda } from "@langchain/core/runnables";
import "@langchain/core/tools";

// ../../node_modules/.pnpm/@langchain+langgraph-checkpoint@1.1.5_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@_ff898d037cda6bb5f88aa9c9d1caaf93/node_modules/@langchain/langgraph-checkpoint/dist/id.js
import { v5, v6 } from "@langchain/core/utils/uuid";

// ../../node_modules/.pnpm/@langchain+langgraph-checkpoint@1.1.5_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@_ff898d037cda6bb5f88aa9c9d1caaf93/node_modules/@langchain/langgraph-checkpoint/dist/serde/types.js
var ERROR = "__error__";
var SCHEDULED = "__scheduled__";
var INTERRUPT = "__interrupt__";
var RESUME = "__resume__";

// ../../node_modules/.pnpm/@langchain+langgraph-checkpoint@1.1.5_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@_ff898d037cda6bb5f88aa9c9d1caaf93/node_modules/@langchain/langgraph-checkpoint/dist/serde/jsonplus.js
import { load } from "@langchain/core/load";

// ../../node_modules/.pnpm/@langchain+langgraph-checkpoint@1.1.5_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@_ff898d037cda6bb5f88aa9c9d1caaf93/node_modules/@langchain/langgraph-checkpoint/dist/base.js
var WRITES_IDX_MAP = {
  [ERROR]: -1,
  [SCHEDULED]: -2,
  [INTERRUPT]: -3,
  [RESUME]: -4
};

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/tests/utils.js
import "zod/v3";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/annotation.js
import { MessagesValue, ReducedValue, StateSchema, UntrackedValue } from "@langchain/langgraph";
import { schemaMetaRegistry } from "@langchain/langgraph/zod";
import { getInteropZodObjectShape, isInteropZodObject as isInteropZodObject2, isZodSchemaV4 } from "@langchain/core/utils/types";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/utils.js
import { AIMessage as AIMessage3, AIMessageChunk, SystemMessage, ToolMessage as ToolMessage3 } from "@langchain/core/messages";
import { Runnable, RunnableBinding as RunnableBinding2, RunnableSequence } from "@langchain/core/runnables";
import { StateSchema as StateSchema2, isCommand } from "@langchain/langgraph";
import { interopParse, isInteropZodSchema as isInteropZodSchema2 } from "@langchain/core/utils/types";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/nodes/utils.js
import { END, ReducedValue as ReducedValue2, StateSchema as StateSchema3 } from "@langchain/langgraph";
import { getInteropZodObjectShape as getInteropZodObjectShape2, interopSafeParseAsync, interopZodObjectMakeFieldsOptional, interopZodObjectPartial, isInteropZodObject as isInteropZodObject3, isZodSchemaV4 as isZodSchemaV42 } from "@langchain/core/utils/types";
import { z } from "zod/v4";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/RunnableCallable.js
import { Runnable as Runnable2, mergeConfigs } from "@langchain/core/runnables";
import { AsyncLocalStorageProviderSingleton } from "@langchain/core/singletons";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/withAgentName.js
import { RunnableLambda as RunnableLambda2, RunnableSequence as RunnableSequence2 } from "@langchain/core/runnables";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/nodes/AgentNode.js
import { AIMessage as AIMessage4, SystemMessage as SystemMessage2, ToolMessage as ToolMessage4 } from "@langchain/core/messages";
import { raceWithSignal } from "@langchain/core/runnables";
import { Command, isCommand as isCommand2 } from "@langchain/langgraph";
import { getSchemaDescription, interopParse as interopParse2 } from "@langchain/core/utils/types";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/nodes/ToolNode.js
import { AIMessage as AIMessage5, BaseMessage, ToolMessage as ToolMessage5 } from "@langchain/core/messages";
import { ToolInputParsingException } from "@langchain/core/tools";
import { Command as Command2, Send, isCommand as isCommand3, isGraphInterrupt } from "@langchain/langgraph";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/nodes/middleware.js
import { getInteropZodObjectShape as getInteropZodObjectShape3, interopParse as interopParse3, isInteropZodObject as isInteropZodObject4 } from "@langchain/core/utils/types";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/ReactAgent.js
import { AIMessage as AIMessage6, ToolMessage as ToolMessage6 } from "@langchain/core/messages";
import { mergeConfigs as mergeConfigs2 } from "@langchain/core/runnables";
import { Command as Command3, END as END2, START, Send as Send2, StateGraph } from "@langchain/langgraph";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/hitl.js
import { AIMessage as AIMessage7, ToolMessage as ToolMessage7 } from "@langchain/core/messages";
import { interrupt } from "@langchain/langgraph";
import { interopParse as interopParse4 } from "@langchain/core/utils/types";
import { z as z2 } from "zod/v3";
var WhenFunctionSchema = z2.function().args(z2.custom()).returns(z2.union([z2.boolean(), z2.promise(z2.boolean())]));
var DescriptionFunctionSchema = z2.function().args(z2.custom(), z2.custom(), z2.custom()).returns(z2.union([z2.string(), z2.promise(z2.string())]));
var ALLOWED_DECISIONS = [
  "approve",
  "edit",
  "reject"
];
var DecisionType = z2.enum(ALLOWED_DECISIONS);
var InterruptOnConfigSchema = z2.object({
  /**
  * The decisions that are allowed for this action.
  */
  allowedDecisions: z2.array(DecisionType),
  /**
  * The description attached to the request for human input.
  * Can be either:
  * - A static string describing the approval request
  * - A callable that dynamically generates the description based on agent state,
  *   runtime, and tool call information
  *
  * @example
  * Static string description
  * ```typescript
  * import type { InterruptOnConfig } from "langchain";
  *
  * const config: InterruptOnConfig = {
  *   allowedDecisions: ["approve", "reject"],
  *   description: "Please review this tool execution"
  * };
  * ```
  *
  * @example
  * Dynamic callable description
  * ```typescript
  * import type {
  *   AgentBuiltInState,
  *   Runtime,
  *   DescriptionFactory,
  *   ToolCall,
  *   InterruptOnConfig
  * } from "langchain";
  *
  * const formatToolDescription: DescriptionFactory = (
  *   toolCall: ToolCall,
  *   state: AgentBuiltInState,
  *   runtime: Runtime<unknown>
  * ) => {
  *   return `Tool: ${toolCall.name}\nArguments:\n${JSON.stringify(toolCall.args, null, 2)}`;
  * };
  *
  * const config: InterruptOnConfig = {
  *   allowedDecisions: ["approve", "edit"],
  *   description: formatToolDescription
  * };
  * ```
  */
  description: z2.union([z2.string(), DescriptionFunctionSchema]).optional(),
  /**
  * JSON schema for the arguments associated with the action, if edits are allowed.
  */
  argsSchema: z2.record(z2.any()).optional(),
  /**
  * Optional predicate controlling whether to interrupt for a given tool call.
  *
  * Receives a {@link ToolCallRequest} and returns `true` to interrupt or
  * `false` to auto-approve the tool call.
  *
  * The request is constructed with `tool` set to `undefined` and `runtime` set
  * to the node-level {@link Runtime}, so `request.tool` is not available.
  *
  * @example
  * ```typescript
  * import type { InterruptOnConfig } from "langchain";
  *
  * // Only interrupt delete_file calls targeting /etc
  * const config: InterruptOnConfig = {
  *   allowedDecisions: ["approve", "reject"],
  *   when: (request) =>
  *     String(request.toolCall.args.path ?? "").startsWith("/etc"),
  * };
  * ```
  */
  when: WhenFunctionSchema.optional()
});
var contextSchema = z2.object({
  /**
  * Mapping of tool name to allowed reviewer responses.
  * If a tool doesn't have an entry, it's auto-approved by default.
  *
  * - `true` -> pause for approval and allow approve/edit/reject decisions
  * - `false` -> auto-approve (no human review)
  * - `InterruptOnConfig` -> explicitly specify which decisions are allowed for this tool
  */
  interruptOn: z2.record(z2.union([z2.boolean(), InterruptOnConfigSchema])).optional(),
  /**
  * Prefix used when constructing human-facing approval messages.
  * Provides context about the tool call being reviewed; does not change the underlying action.
  *
  * Note: This prefix is only applied for tools that do not provide a custom
  * `description` via their {@link InterruptOnConfig}. If a tool specifies a custom
  * `description`, that per-tool text is used and this prefix is ignored.
  */
  descriptionPrefix: z2.string().default("Tool execution requires approval")
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/constants.js
import { z as z3 } from "zod/v3";
import { getRetryable } from "@langchain/core/errors";
var RetrySchema = z3.object({
  /**
  * Maximum number of retry attempts after the initial call.
  * Default is 2 retries (3 total attempts). Must be >= 0.
  */
  maxRetries: z3.number().min(0).default(2),
  /**
  * Either an array of error constructors to retry on, or a function
  * that takes an error and returns `true` if it should be retried.
  * Default is to retry unless the error is marked non-retryable.
  */
  retryOn: z3.union([z3.function().args(z3.instanceof(Error)).returns(z3.boolean()), z3.array(z3.custom())]).default(() => (error) => getRetryable(error) ?? true),
  /**
  * Multiplier for exponential backoff. Each retry waits
  * `initialDelayMs * (backoffFactor ** retryNumber)` milliseconds.
  * Set to 0.0 for constant delay. Default is 2.0.
  */
  backoffFactor: z3.number().min(0).default(2),
  /**
  * Initial delay in milliseconds before first retry. Default is 1000 (1 second).
  */
  initialDelayMs: z3.number().min(0).default(1e3),
  /**
  * Maximum delay in milliseconds between retries. Caps exponential
  * backoff growth. Default is 60000 (60 seconds).
  */
  maxDelayMs: z3.number().min(0).default(6e4),
  /**
  * Whether to add random jitter (±25%) to delay to avoid thundering herd.
  * Default is `true`.
  */
  jitter: z3.boolean().default(true)
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/summarization.js
import { AIMessage as AIMessage8, HumanMessage as HumanMessage2, RemoveMessage, SystemMessage as SystemMessage3, ToolMessage as ToolMessage8, getBufferString, trimMessages } from "@langchain/core/messages";
import { mergeConfigs as mergeConfigs3, pickRunnableConfigKeys } from "@langchain/core/runnables";
import { REMOVE_ALL_MESSAGES } from "@langchain/langgraph";
import { interopSafeParse } from "@langchain/core/utils/types";
import { z as z4 } from "zod/v4";
import { z as z$1 } from "zod/v3";
import { v4 } from "@langchain/core/utils/uuid";
import { getModelContextSize } from "@langchain/core/language_models/base";
var DEFAULT_SUMMARY_PROMPT = `<role>
Context Extraction Assistant
</role>

<primary_objective>
Your sole objective in this task is to extract the highest quality/most relevant context from the conversation history below.
</primary_objective>

<objective_information>
You're nearing the total number of input tokens you can accept, so you must extract the highest quality/most relevant pieces of information from your conversation history.
This context will then overwrite the conversation history presented below. Because of this, ensure the context you extract is only the most important information to your overall goal.
</objective_information>

<instructions>
The conversation history below will be replaced with the context you extract in this step. Because of this, you must do your very best to extract and record all of the most important context from the conversation history.
You want to ensure that you don't repeat any actions you've already completed, so the context you extract from the conversation history should be focused on the most important information to your overall goal.
</instructions>

The user will message you with the full message history you'll be extracting context from, to then replace. Carefully read over it all, and think deeply about what information is most important to your overall goal that should be saved:

With all of this in mind, please carefully read over the entire conversation history, and extract the most important and relevant context to replace it so that you can free up space in the conversation history.
Respond ONLY with the extracted context. Do not include any additional information, or text before or after the extracted context.

<messages>
Messages to summarize:
{messages}
</messages>`;
var tokenCounterSchema = z$1.function().args(z$1.array(z$1.custom())).returns(z$1.union([z$1.number(), z$1.promise(z$1.number())]));
var contextSizeSchema = z$1.object({
  /**
  * Fraction of the model's context size to use as the trigger
  */
  fraction: z$1.number().gt(0, "Fraction must be greater than 0").max(1, "Fraction must be less than or equal to 1").optional(),
  /**
  * Number of tokens to use as the trigger
  */
  tokens: z$1.number().positive("Tokens must be greater than 0").optional(),
  /**
  * Number of messages to use as the trigger
  */
  messages: z$1.number().int("Messages must be an integer").positive("Messages must be greater than 0").optional()
}).refine((data) => {
  return [
    data.fraction,
    data.tokens,
    data.messages
  ].filter((v) => v !== void 0).length >= 1;
}, { message: "At least one of fraction, tokens, or messages must be provided" });
var keepSchema = z$1.object({
  /**
  * Fraction of the model's context size to keep
  */
  fraction: z$1.number().min(0, "Messages must be non-negative").max(1, "Fraction must be less than or equal to 1").optional(),
  /**
  * Number of tokens to keep
  */
  tokens: z$1.number().min(0, "Tokens must be greater than or equal to 0").optional(),
  messages: z$1.number().int("Messages must be an integer").min(0, "Messages must be non-negative").optional()
}).refine((data) => {
  return [
    data.fraction,
    data.tokens,
    data.messages
  ].filter((v) => v !== void 0).length === 1;
}, { message: "Exactly one of fraction, tokens, or messages must be provided" });
var contextSchema2 = z$1.object({
  /**
  * Model to use for summarization
  */
  model: z$1.custom(),
  /**
  * Trigger conditions for summarization.
  * Can be a single condition object (all properties must be met) or an array of conditions (any condition must be met).
  *
  * @example
  * ```ts
  * // Single condition: trigger if tokens >= 5000 AND messages >= 3
  * trigger: { tokens: 5000, messages: 3 }
  *
  * // Multiple conditions: trigger if (tokens >= 5000 AND messages >= 3) OR (tokens >= 3000 AND messages >= 6)
  * trigger: [
  *   { tokens: 5000, messages: 3 },
  *   { tokens: 3000, messages: 6 }
  * ]
  * ```
  */
  trigger: z$1.union([contextSizeSchema, z$1.array(contextSizeSchema)]).optional(),
  /**
  * Keep conditions for summarization
  */
  keep: keepSchema.optional(),
  /**
  * Token counter function to use for summarization
  */
  tokenCounter: tokenCounterSchema.optional(),
  /**
  * Summary prompt to use for summarization
  * @default {@link DEFAULT_SUMMARY_PROMPT}
  */
  summaryPrompt: z$1.string().default(DEFAULT_SUMMARY_PROMPT),
  /**
  * Number of tokens to trim to before summarizing
  */
  trimTokensToSummarize: z$1.number().optional(),
  /**
  * Prefix to add to the summary
  */
  summaryPrefix: z$1.string().optional(),
  /**
  * @deprecated Use `trigger: { tokens: value }` instead.
  */
  maxTokensBeforeSummary: z$1.number().optional(),
  /**
  * @deprecated Use `keep: { messages: value }` instead.
  */
  messagesToKeep: z$1.number().optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/dynamicSystemPrompt.js
import { SystemMessage as SystemMessage4 } from "@langchain/core/messages";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/llmToolSelector.js
import { HumanMessage as HumanMessage3 } from "@langchain/core/messages";
import { mergeConfigs as mergeConfigs4, pickRunnableConfigKeys as pickRunnableConfigKeys2 } from "@langchain/core/runnables";
import { z as z5 } from "zod/v3";
import { BaseLanguageModel } from "@langchain/core/language_models/base";
var LLMToolSelectorOptionsSchema = z5.object({
  /**
  * The language model to use for tool selection (default: the provided model from the agent options).
  */
  model: z5.string().or(z5.instanceof(BaseLanguageModel)).optional(),
  /**
  * System prompt for the tool selection model.
  */
  systemPrompt: z5.string().optional(),
  /**
  * Maximum number of tools to select. If the model selects more,
  * only the first maxTools will be used. No limit if not specified.
  */
  maxTools: z5.number().optional(),
  /**
  * Tool names to always include regardless of selection.
  * These do not count against the maxTools limit.
  */
  alwaysInclude: z5.array(z5.string()).optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/pii.js
import { AIMessage as AIMessage9, HumanMessage as HumanMessage4, ToolMessage as ToolMessage9 } from "@langchain/core/messages";
import { z as z6 } from "zod/v3";
import { sha256 } from "@langchain/core/utils/hash";
var contextSchema3 = z6.object({
  /**
  * Whether to check user messages before model call
  */
  applyToInput: z6.boolean().optional(),
  /**
  * Whether to check AI messages after model call
  */
  applyToOutput: z6.boolean().optional(),
  /**
  * Whether to check tool result messages after tool execution
  */
  applyToToolResults: z6.boolean().optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/piiRedaction.js
import { AIMessage as AIMessage10, HumanMessage as HumanMessage5, RemoveMessage as RemoveMessage2, SystemMessage as SystemMessage5, ToolMessage as ToolMessage10 } from "@langchain/core/messages";
import { z as z7 } from "zod/v3";
var contextSchema4 = z7.object({
  /**
  * A record of PII detection rules to apply
  * @default DEFAULT_PII_RULES (with enabled rules only)
  */
  rules: z7.record(z7.string(), z7.instanceof(RegExp).describe("Regular expression pattern to match PII")).optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/contextEditing.js
import { AIMessage as AIMessage11, SystemMessage as SystemMessage6, ToolMessage as ToolMessage11 } from "@langchain/core/messages";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/toolCallLimit.js
import { AIMessage as AIMessage12, ToolMessage as ToolMessage12 } from "@langchain/core/messages";
import { z as z8 } from "zod/v4";
import { z as z$12 } from "zod/v3";
var VALID_EXIT_BEHAVIORS = [
  "continue",
  "error",
  "end"
];
var DEFAULT_EXIT_BEHAVIOR = "continue";
var exitBehaviorSchema = z$12.enum(VALID_EXIT_BEHAVIORS).default(DEFAULT_EXIT_BEHAVIOR);
z$12.object({
  /**
  * Name of the specific tool to limit. If undefined, limits apply to all tools.
  */
  toolName: z$12.string().optional(),
  /**
  * Maximum number of tool calls allowed per thread.
  * undefined means no limit.
  */
  threadLimit: z$12.number().optional(),
  /**
  * Maximum number of tool calls allowed per run.
  * undefined means no limit.
  */
  runLimit: z$12.number().optional(),
  /**
  * What to do when limits are exceeded.
  * - "continue": Block exceeded tools with error messages, let other tools continue (default)
  * - "error": Raise a ToolCallLimitExceededError exception
  * - "end": Stop execution immediately, injecting a ToolMessage and an AI message
  *   for the single tool call that exceeded the limit. Raises NotImplementedError
  *   if there are multiple tool calls.
  *
  * @default "continue"
  */
  exitBehavior: exitBehaviorSchema
});
var stateSchema = z$12.object({
  threadToolCallCount: z$12.record(z$12.string(), z$12.number()).default({}),
  runToolCallCount: z$12.record(z$12.string(), z$12.number()).default({})
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/todoListMiddleware.js
import { AIMessage as AIMessage13, ToolMessage as ToolMessage13 } from "@langchain/core/messages";
import { tool as tool2 } from "@langchain/core/tools";
import { Command as Command4 } from "@langchain/langgraph";
import { z as z9 } from "zod/v3";
var TodoStatus = z9.enum([
  "pending",
  "in_progress",
  "completed"
]).describe("Status of the todo");
var TodoSchema = z9.object({
  content: z9.string().describe("Content of the todo item"),
  status: TodoStatus
});
var stateSchema2 = z9.object({ todos: z9.array(TodoSchema).default([]) });

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/modelCallLimit.js
import { AIMessage as AIMessage14 } from "@langchain/core/messages";
import { z as z10 } from "zod/v3";
var contextSchema5 = z10.object({
  /**
  * The maximum number of model calls allowed per thread.
  */
  threadLimit: z10.number().optional(),
  /**
  * The maximum number of model calls allowed per run.
  */
  runLimit: z10.number().optional(),
  /**
  * The behavior to take when the limit is exceeded.
  * - "error" will throw an error and stop the agent.
  * - "end" will end the agent.
  * @default "end"
  */
  exitBehavior: z10.enum(["error", "end"]).optional()
});
var stateSchema3 = z10.object({
  threadModelCallCount: z10.number().default(0),
  runModelCallCount: z10.number().default(0)
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/error.js
import { z as z11 } from "zod/v4";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/modelRetry.js
import { AIMessage as AIMessage15 } from "@langchain/core/messages";
import { z as z12 } from "zod/v3";
var ModelRetryMiddlewareOptionsSchema = z12.object({
  /**
  * Behavior when all retries are exhausted. Options:
  * - `"continue"` (default): Return an AIMessage with error details, allowing
  *   the agent to potentially handle the failure gracefully.
  * - `"error"`: Re-raise the exception, stopping agent execution.
  * - Custom function: Function that takes the exception and returns a string
  *   for the AIMessage content, allowing custom error formatting.
  */
  onFailure: z12.union([
    z12.literal("error"),
    z12.literal("continue"),
    z12.function().args(z12.instanceof(Error)).returns(z12.string())
  ]).default("continue")
}).merge(RetrySchema);

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/toolRetry.js
import { ToolMessage as ToolMessage14 } from "@langchain/core/messages";
import { z as z13 } from "zod/v3";
var ToolRetryMiddlewareOptionsSchema = z13.object({
  /**
  * Optional list of tools or tool names to apply retry logic to.
  * Can be a list of `BaseTool` instances or tool name strings.
  * If `undefined`, applies to all tools. Default is `undefined`.
  */
  tools: z13.array(z13.union([
    z13.custom(),
    z13.custom(),
    z13.string()
  ])).optional(),
  /**
  * Behavior when all retries are exhausted. Options:
  * - `"continue"` (default): Return an AIMessage with error details, allowing
  *   the agent to potentially handle the failure gracefully.
  * - `"error"`: Re-raise the exception, stopping agent execution.
  * - Custom function: Function that takes the exception and returns a string
  *   for the AIMessage content, allowing custom error formatting.
  *
  * Deprecated values:
  * - `"raise"`: use `"error"` instead.
  * - `"return_message"`: use `"continue"` instead.
  */
  onFailure: z13.union([
    z13.literal("error"),
    z13.literal("continue"),
    z13.literal("raise"),
    z13.literal("return_message"),
    z13.function().args(z13.instanceof(Error)).returns(z13.string())
  ]).default("continue")
}).merge(RetrySchema);

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/toolError.js
import { ToolMessage as ToolMessage15 } from "@langchain/core/messages";
import { isGraphBubbleUp as isGraphBubbleUp2 } from "@langchain/langgraph";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/toolEmulator.js
import { HumanMessage as HumanMessage6, ToolMessage as ToolMessage16 } from "@langchain/core/messages";
import { mergeConfigs as mergeConfigs5, pickRunnableConfigKeys as pickRunnableConfigKeys3 } from "@langchain/core/runnables";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/providerToolSearch.js
import { isLangChainTool as isLangChainTool2 } from "@langchain/core/utils/function_calling";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/provider/openai/moderation.js
import { AIMessage as AIMessage16, HumanMessage as HumanMessage7, ToolMessage as ToolMessage17 } from "@langchain/core/messages";

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/provider/anthropic/promptCaching.js
import { z as z14 } from "zod/v3";
var contextSchema6 = z14.object({
  /**
  * Whether to enable prompt caching.
  * @default true
  */
  enableCaching: z14.boolean().optional(),
  /**
  * The time-to-live for the cached prompt.
  * @default "5m"
  */
  ttl: z14.enum(["5m", "1h"]).optional(),
  /**
  * The minimum number of messages required before caching is applied.
  * @default 3
  */
  minMessagesToCache: z14.number().optional(),
  /**
  * The behavior to take when an unsupported model is used.
  * - "ignore" will ignore the unsupported model and continue without caching.
  * - "warn" will warn the user and continue without caching.
  * - "raise" will raise an error and stop the agent.
  * @default "warn"
  */
  unsupportedModelBehavior: z14.enum([
    "ignore",
    "warn",
    "raise"
  ]).optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/agents/middleware/provider/aws/promptCaching.js
import { z as z15 } from "zod/v3";
var contextSchema7 = z15.object({
  /**
  * Whether to enable prompt caching.
  * @default true
  */
  enableCaching: z15.boolean().optional(),
  /**
  * The time-to-live for the cached prompt.
  * @default "5m"
  */
  ttl: z15.enum(["5m", "1h"]).optional(),
  /**
  * The minimum number of messages required before caching is applied.
  * @default 1
  */
  minMessagesToCache: z15.number().optional(),
  /**
  * The behavior to take when an unsupported model is used.
  * - "ignore" will ignore the unsupported model and continue without caching.
  * - "warn" will warn the user and continue without caching.
  * - "raise" will raise an error and stop the agent.
  * @default "warn"
  */
  unsupportedModelBehavior: z15.enum([
    "ignore",
    "warn",
    "raise"
  ]).optional()
});

// ../../node_modules/.pnpm/langchain@1.5.11_@langchain+core@1.2.11_@opentelemetry+api@1.9.1_@opentelemetry+exporte_f5289af23b1dceae16b2f90e7460a344/node_modules/langchain/dist/index.js
import { AIMessage as AIMessage17, AIMessageChunk as AIMessageChunk2, BaseMessage as BaseMessage2, BaseMessageChunk, HumanMessage as HumanMessage8, HumanMessageChunk, SystemMessage as SystemMessage7, SystemMessageChunk, ToolMessage as ToolMessage18, ToolMessageChunk, filterMessages, trimMessages as trimMessages2 } from "@langchain/core/messages";
import { DynamicStructuredTool, DynamicTool, StructuredTool, Tool } from "@langchain/core/tools";
import { context } from "@langchain/core/utils/context";
import { InMemoryStore as InMemoryStore2 } from "@langchain/core/stores";
import { Document } from "@langchain/core/documents";
import { fakeModel, langchainMatchers } from "@langchain/core/testing";

// src/bridge/approvals.ts
var NATIVE_TOOL_KIND = {
  read_file: "readonly",
  write_file: "edit",
  edit_file: "edit",
  execute: "bash",
  grep: "readonly",
  glob: "readonly",
  ls: "readonly",
  task: "edit",
  write_todos: "edit"
};
var NATIVE_TO_COMMON = {
  read_file: "read",
  write_file: "write",
  edit_file: "edit",
  execute: "bash"
};
function toCommonName(nativeName) {
  return NATIVE_TO_COMMON[nativeName] ?? nativeName;
}
function isBuiltinToolIncluded(input) {
  if (input.toolFiltering == null) return true;
  const toolName = toCommonName(input.nativeName);
  return input.toolFiltering.mode === "allow" ? input.toolFiltering.toolNames.includes(toolName) : !input.toolFiltering.toolNames.includes(toolName);
}
function builtinToolRequiresApproval(kind, permissionMode) {
  if (permissionMode === "allow-all") return false;
  if (permissionMode === "allow-edits") return kind === "bash";
  return kind === "edit" || kind === "bash";
}
function buildInterruptOn(permissionMode, builtinToolFiltering) {
  const config = {};
  for (const [nativeName, kind] of Object.entries(NATIVE_TOOL_KIND)) {
    if (permissionMode != null && isBuiltinToolIncluded({
      nativeName,
      toolFiltering: builtinToolFiltering
    }) && builtinToolRequiresApproval(kind, permissionMode)) {
      config[nativeName] = { allowedDecisions: ["approve", "reject"] };
    }
  }
  return Object.keys(config).length > 0 ? config : void 0;
}
function collectActionRequests(interrupts) {
  const out = [];
  for (const interrupt2 of interrupts) {
    const value = interrupt2.value;
    for (const action of value?.actionRequests ?? []) {
      out.push({ name: action.name, args: action.args ?? {} });
    }
  }
  return out;
}

// src/bridge/create-emit-stream-event.ts
import { randomUUID as randomUUID2 } from "crypto";
function createDeepAgentsStreamEventState() {
  return {
    streamStarted: false,
    textBlockId: void 0,
    reasoningBlockId: void 0,
    inputTokens: 0,
    outputTokens: 0,
    streamedStepInput: 0,
    streamedStepOutput: 0,
    pendingStep: void 0,
    approvedToolQueue: /* @__PURE__ */ new Map(),
    approvedRunIds: /* @__PURE__ */ new Map(),
    dynamicToolRunIds: /* @__PURE__ */ new Set()
  };
}
var NATIVE_TO_COMMON2 = {
  read_file: "read",
  write_file: "write",
  edit_file: "edit",
  execute: "bash"
};
function toCommonName2(nativeName) {
  return NATIVE_TO_COMMON2[nativeName] ?? nativeName;
}
function createEmitStreamEvent({
  state,
  configuredModel,
  hostToolNames,
  mcpToolNames: mcpToolNames2,
  structuredOutputToolNames = /* @__PURE__ */ new Set(),
  emit
}) {
  return (event) => {
    const kind = event.event;
    const data = event.data ?? {};
    const ns = event.metadata?.langgraph_checkpoint_ns ?? "";
    const nested = ns.includes("|");
    if (kind === "on_chat_model_start") {
      if (!nested) {
        if (!state.streamStarted) {
          state.streamStarted = true;
          const modelId = resolveDeepAgentsModelId({
            configuredModel,
            metadata: event.metadata
          });
          emit({
            type: "stream-start",
            ...modelId ? { modelId } : {}
          });
        }
        flushStep({ state, emit });
      }
    } else if (kind === "on_chat_model_stream") {
      if (nested) return;
      const chunk = data.chunk;
      if (!chunk) return;
      const content = chunk.content;
      if (typeof content === "string" && content) {
        emitText({ state, emit, delta: content });
      } else if (Array.isArray(content)) {
        for (const block of content) {
          if (block && typeof block === "object") {
            const value = block;
            if ((value.type === "text" || value.type === "text-delta") && value.text) {
              emitText({ state, emit, delta: value.text });
            } else if (value.type === "thinking" && value.thinking) {
              emitReasoning({ state, emit, delta: value.thinking });
            } else if ((value.type === "reasoning" || value.type === "reasoning-delta") && value.reasoning) {
              emitReasoning({ state, emit, delta: value.reasoning });
            }
          }
        }
      }
      const usage = chunk.usage_metadata;
      if (usage) {
        state.streamedStepInput = Math.max(
          state.streamedStepInput,
          usage.input_tokens ?? 0
        );
        state.streamedStepOutput = Math.max(
          state.streamedStepOutput,
          usage.output_tokens ?? 0
        );
      }
    } else if (kind === "on_chat_model_end") {
      const output = data.output;
      const usage = output?.usage_metadata;
      const stepInput = usage?.input_tokens ?? state.streamedStepInput;
      const stepOutput = usage?.output_tokens ?? state.streamedStepOutput;
      state.inputTokens += stepInput;
      state.outputTokens += stepOutput;
      state.streamedStepInput = 0;
      state.streamedStepOutput = 0;
      if (!nested) {
        endTextBlock({ state, emit });
        endReasoningBlock({ state, emit });
        state.pendingStep = { input: stepInput, output: stepOutput };
      }
    } else if (kind === "on_tool_start") {
      const toolName = event.name ?? "unknown";
      if (structuredOutputToolNames.has(toolName)) return;
      const runId = event.run_id ?? "";
      if (!nested && !hostToolNames.has(toolName)) {
        const isMcpTool = mcpToolNames2.has(toolName);
        if (isMcpTool && runId) state.dynamicToolRunIds.add(runId);
        const queued = state.approvedToolQueue.get(toolName);
        if (queued && queued.length > 0) {
          const approvalId = queued.shift();
          if (runId) state.approvedRunIds.set(runId, approvalId);
        } else {
          endTextBlock({ state, emit });
          endReasoningBlock({ state, emit });
          emit({
            type: "tool-call",
            toolCallId: runId,
            toolName: toCommonName2(toolName),
            input: toToolCallInput(data.input),
            providerExecuted: true,
            nativeName: toolName,
            ...isMcpTool ? { dynamic: true } : {}
          });
        }
      }
    } else if (kind === "on_tool_end") {
      const toolName = event.name ?? "unknown";
      if (structuredOutputToolNames.has(toolName)) return;
      const runId = event.run_id ?? "";
      if (!nested && !hostToolNames.has(toolName)) {
        const dynamic = state.dynamicToolRunIds.delete(runId);
        let output = data.output ?? "";
        if (output && typeof output === "object" && "content" in output) {
          output = output.content;
        }
        emit({
          type: "tool-result",
          toolCallId: state.approvedRunIds.get(runId) ?? runId,
          toolName: toCommonName2(toolName),
          result: output ?? null,
          ...dynamic ? { dynamic: true } : {}
        });
        state.approvedRunIds.delete(runId);
      }
    }
  };
}
function endTextBlock({
  state,
  emit
}) {
  if (state.textBlockId) {
    emit({ type: "text-end", id: state.textBlockId });
    state.textBlockId = void 0;
  }
}
function endReasoningBlock({
  state,
  emit
}) {
  if (state.reasoningBlockId) {
    emit({ type: "reasoning-end", id: state.reasoningBlockId });
    state.reasoningBlockId = void 0;
  }
}
function flushStep({
  state,
  emit
}) {
  if (!state.pendingStep) return;
  emit({
    type: "finish-step",
    finishReason: { unified: "stop" },
    usage: {
      inputTokens: { total: state.pendingStep.input },
      outputTokens: { total: state.pendingStep.output }
    }
  });
  state.pendingStep = void 0;
}
function ensureTextBlock({
  state,
  emit
}) {
  if (!state.textBlockId) {
    state.textBlockId = `text-${randomUUID2()}`;
    emit({ type: "text-start", id: state.textBlockId });
  }
  return state.textBlockId;
}
function emitText({
  state,
  emit,
  delta
}) {
  endReasoningBlock({ state, emit });
  emit({ type: "text-delta", id: ensureTextBlock({ state, emit }), delta });
}
function emitReasoning({
  state,
  emit,
  delta
}) {
  endTextBlock({ state, emit });
  if (!state.reasoningBlockId) {
    state.reasoningBlockId = `reasoning-${randomUUID2()}`;
    emit({ type: "reasoning-start", id: state.reasoningBlockId });
  }
  emit({ type: "reasoning-delta", id: state.reasoningBlockId, delta });
}
function resolveDeepAgentsModelId({
  configuredModel,
  metadata
}) {
  if (metadata && typeof metadata === "object" && !Array.isArray(metadata)) {
    const modelId = metadata.ls_model_name;
    if (typeof modelId === "string" && modelId.length > 0) return modelId;
  }
  return configuredModel;
}
function toToolCallInput(raw) {
  if (raw && typeof raw === "object" && !Array.isArray(raw) && Object.keys(raw).length === 1 && typeof raw.input === "string") {
    const inner = raw.input;
    if (/^\s*[[{]/.test(inner)) return inner;
  }
  return JSON.stringify(raw ?? {});
}

// src/bridge/json-schema-to-zod.ts
import { z as z16 } from "zod/v4";
function jsonSchemaToZodObject(input) {
  const schema = input && typeof input === "object" ? input : {};
  return z16.object(toZodShape(schema));
}
function toZodShape(schema) {
  if (!schema.properties) return {};
  const required = new Set(schema.required);
  const shape = {};
  for (const [key, propSchema] of Object.entries(schema.properties)) {
    const propType = toZodType(propSchema);
    shape[key] = required.has(key) ? propType : propType.optional();
  }
  return shape;
}
function toZodType(schema) {
  if (!schema) return z16.any();
  const types = Array.isArray(schema.type) ? schema.type.filter((t) => t !== "null") : [schema.type].filter(Boolean);
  let zType;
  switch (types[0]) {
    case "string":
      zType = z16.string();
      break;
    case "number":
      zType = z16.number();
      break;
    case "integer":
      zType = z16.number().int();
      break;
    case "boolean":
      zType = z16.boolean();
      break;
    case "array":
      zType = z16.array(toZodType(schema.items));
      break;
    case "object":
      zType = z16.object(toZodShape(schema));
      break;
    case "null":
      zType = z16.null();
      break;
    default:
      zType = z16.any();
  }
  if (schema.description) zType = zType.describe(schema.description);
  if (schema.nullable) zType = zType.nullable();
  return zType;
}

// src/bridge/local-shell-backend.ts
import { LocalShellBackend } from "deepagents";
var SANDBOX_PATH_FALLBACK = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin";
function createLocalShellBackend({
  rootDir,
  env = process.env
}) {
  return new LocalShellBackend({
    rootDir,
    env: {
      PATH: env.PATH ?? SANDBOX_PATH_FALLBACK
    }
  });
}

// src/bridge/persistent-memory-saver.ts
import { mkdir as mkdir2, readFile, rename, rm, writeFile as writeFile2 } from "fs/promises";
import { dirname } from "path";
var SNAPSHOT_HEADER = "deepagents-memory-saver-v1";
function encodeString(value) {
  return Buffer.from(value, "utf8").toString("base64");
}
function decodeString(value) {
  return Buffer.from(value, "base64").toString("utf8");
}
function encodeBytes(value) {
  return Buffer.from(value).toString("base64");
}
function decodeBytes(value) {
  return Buffer.from(value, "base64");
}
async function loadMemorySaver({
  path,
  saver
}) {
  let snapshot;
  try {
    snapshot = await readFile(path, "utf8");
  } catch (error) {
    if (error.code === "ENOENT") return;
    throw error;
  }
  const [header, ...lines] = snapshot.split("\n");
  if (header !== SNAPSHOT_HEADER) {
    throw new Error("Unsupported Deep Agents conversation checkpoint format");
  }
  const storage = /* @__PURE__ */ Object.create(null);
  const writes = /* @__PURE__ */ Object.create(null);
  for (const line of lines) {
    if (line === "") continue;
    const fields = line.split("	");
    if (fields[0] === "S" && fields.length === 7) {
      const [, threadIdValue, namespaceValue, checkpointIdValue] = fields;
      const threadId = decodeString(threadIdValue);
      const namespace = decodeString(namespaceValue);
      const checkpointId = decodeString(checkpointIdValue);
      storage[threadId] ??= /* @__PURE__ */ Object.create(null);
      storage[threadId][namespace] ??= /* @__PURE__ */ Object.create(null);
      storage[threadId][namespace][checkpointId] = [
        decodeBytes(fields[4]),
        decodeBytes(fields[5]),
        fields[6] === "" ? void 0 : decodeString(fields[6])
      ];
      continue;
    }
    if (fields[0] === "W" && fields.length === 6) {
      const [, keyValue, indexValue, taskIdValue, channelValue, value] = fields;
      const key = decodeString(keyValue);
      const index = decodeString(indexValue);
      writes[key] ??= /* @__PURE__ */ Object.create(null);
      writes[key][index] = [
        decodeString(taskIdValue),
        decodeString(channelValue),
        decodeBytes(value)
      ];
      continue;
    }
    throw new Error("Invalid Deep Agents conversation checkpoint");
  }
  saver.storage = storage;
  saver.writes = writes;
}
async function saveMemorySaver({
  path,
  saver
}) {
  const lines = [SNAPSHOT_HEADER];
  for (const [threadId, namespaces] of Object.entries(saver.storage)) {
    for (const [namespace, checkpoints] of Object.entries(namespaces)) {
      for (const [
        checkpointId,
        [checkpoint, metadata, parentCheckpointId]
      ] of Object.entries(checkpoints)) {
        lines.push(
          [
            "S",
            encodeString(threadId),
            encodeString(namespace),
            encodeString(checkpointId),
            encodeBytes(checkpoint),
            encodeBytes(metadata),
            parentCheckpointId == null ? "" : encodeString(parentCheckpointId)
          ].join("	")
        );
      }
    }
  }
  for (const [key, indexedWrites] of Object.entries(saver.writes)) {
    for (const [index, [taskId, channel, value]] of Object.entries(
      indexedWrites
    )) {
      lines.push(
        [
          "W",
          encodeString(key),
          encodeString(index),
          encodeString(taskId),
          encodeString(channel),
          encodeBytes(value)
        ].join("	")
      );
    }
  }
  await mkdir2(dirname(path), { recursive: true });
  const temporaryPath = `${path}.${process.pid}.tmp`;
  await writeFile2(temporaryPath, `${lines.join("\n")}
`, "utf8");
  await rename(temporaryPath, path);
}
async function removeMemorySaverSnapshot(path) {
  await rm(path, { force: true });
}

// src/bridge/tool-filtering.ts
import {
  AIMessage as AIMessage18,
  ToolMessage as ToolMessage19
} from "@langchain/core/messages";
var MIDDLEWARE_BRAND2 = /* @__PURE__ */ Symbol.for("AgentMiddleware");
function getBuiltinToolFilteringDenialReason(input) {
  return `Tool '${input.toolName}' is inactive due to the HarnessAgent tool filtering policy.`;
}
function stringifyToolInput(input) {
  return JSON.stringify(input ?? {});
}
function isNativeBuiltinToolCall(input) {
  return Object.keys(NATIVE_TOOL_KIND).includes(input.toolCall.name);
}
function isInactiveBuiltinToolCall(input) {
  return isNativeBuiltinToolCall({ toolCall: input.toolCall }) && !isBuiltinToolIncluded({
    nativeName: input.toolCall.name,
    toolFiltering: input.builtinToolFiltering
  });
}
function createBuiltinToolFilteringMiddleware(input) {
  if (input.builtinToolFiltering == null) return void 0;
  return {
    [MIDDLEWARE_BRAND2]: true,
    name: "HarnessBuiltinToolFilteringMiddleware",
    afterModel: {
      canJumpTo: ["model"],
      hook: (state) => {
        const lastMessage = [...state.messages].reverse().find((message) => AIMessage18.isInstance(message));
        if (!lastMessage?.tool_calls?.length) return void 0;
        let hasActiveToolCalls = false;
        const deniedToolMessages = [];
        for (const toolCall of lastMessage.tool_calls) {
          if (!isInactiveBuiltinToolCall({
            toolCall,
            builtinToolFiltering: input.builtinToolFiltering
          })) {
            hasActiveToolCalls = true;
            continue;
          }
          const nativeName = toolCall.name;
          toolCall.id ??= `${nativeName}-filtered-${deniedToolMessages.length}`;
          const toolName = toCommonName(nativeName);
          const reason = getBuiltinToolFilteringDenialReason({ toolName });
          input.emit({
            type: "tool-call",
            toolCallId: toolCall.id,
            toolName,
            input: stringifyToolInput(toolCall.args),
            providerExecuted: true,
            nativeName
          });
          input.emit({
            type: "tool-result",
            toolCallId: toolCall.id,
            toolName,
            result: reason
          });
          deniedToolMessages.push(
            new ToolMessage19({
              content: reason,
              name: nativeName,
              tool_call_id: toolCall.id,
              status: "error"
            })
          );
        }
        if (deniedToolMessages.length === 0) return void 0;
        return {
          messages: [lastMessage, ...deniedToolMessages],
          ...hasActiveToolCalls ? {} : { jumpTo: "model" }
        };
      }
    }
  };
}

// src/bridge/index.ts
var HARNESS_CLIENT_APP = procEnv2.AI_SDK_HARNESS_CLIENT_APP;
function parseArgs(rawArgs) {
  const out = {};
  for (let i = 0; i < rawArgs.length; i++) {
    const arg = rawArgs[i];
    if (arg.startsWith("--")) {
      const key = arg.slice(2).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
      out[key] = rawArgs[i + 1];
      i++;
    }
  }
  return out;
}
function buildModel({
  rawModel,
  thinking,
  effort,
  headers
}) {
  if (!rawModel) return void 0;
  const baseUrl = procEnv2.ANTHROPIC_BASE_URL;
  const model = baseUrl ? rawModel : rawModel.replace(/^anthropic[/:]/, "");
  return new ChatAnthropic({
    model,
    ...thinking ? { thinking } : {},
    ...effort ? { outputConfig: { effort } } : {},
    ...procEnv2.ANTHROPIC_API_KEY ? { apiKey: procEnv2.ANTHROPIC_API_KEY } : {},
    ...baseUrl ? { anthropicApiUrl: baseUrl } : {},
    ...headers != null || procEnv2.AI_GATEWAY_API_KEY ? {
      clientOptions: {
        defaultHeaders: {
          ...headers,
          ...procEnv2.AI_GATEWAY_API_KEY && HARNESS_CLIENT_APP ? {
            "User-Agent": HARNESS_CLIENT_APP,
            "x-client-app": HARNESS_CLIENT_APP
          } : {}
        }
      }
    } : {}
  });
}
function createModelMiddleware() {
  return createMiddleware({
    name: "harnessModel",
    wrapModelCall: async (request, handler) => {
      if (!activeModel && !activeThinking && !activeEffort && !activeHeaders) {
        return handler(request);
      }
      if (activeModel) {
        const configuredModel2 = buildModel({
          rawModel: activeModel,
          thinking: activeThinking,
          effort: activeEffort,
          headers: activeHeaders
        });
        if (!configuredModel2) throw new Error("Deep Agents model is missing");
        return handler({ ...request, model: configuredModel2 });
      }
      let model = request.model;
      if ("_getModelInstance" in model && typeof model._getModelInstance === "function") {
        model = await model._getModelInstance();
      }
      if (!(model instanceof ChatAnthropic)) {
        throw new Error("Deep Agents reasoning requires ChatAnthropic");
      }
      const configuredModel = buildModel({
        rawModel: model.model,
        thinking: activeThinking,
        effort: activeEffort,
        headers: activeHeaders
      });
      if (!configuredModel) throw new Error("Deep Agents model is missing");
      return handler({ ...request, model: configuredModel });
    }
  });
}
var args = parseArgs(argv.slice(2));
var workdir = args.workdir;
var bridgeStateDir = args.bridgeStateDir;
if (!workdir || !bridgeStateDir) {
  console.error("deepagents bridge: missing --workdir / --bridge-state-dir");
  process.exit(1);
}
var conversationCheckpointPath = `${bridgeStateDir}/conversation.checkpoint`;
var agent;
var currentTurn;
var mcpClient;
var mcpToolNames = /* @__PURE__ */ new Set();
var currentResponseFormat;
var checkpointer = new MemorySaver2();
if (args.resume === "true") {
  await loadMemorySaver({
    path: conversationCheckpointPath,
    saver: checkpointer
  });
} else {
  await removeMemorySaverSnapshot(conversationCheckpointPath);
}
var agentConfigurationSignature;
var activeModel;
var activeThinking;
var activeEffort;
var activeHeaders;
var modelMiddleware = createModelMiddleware();
var responseFormatMiddleware = createMiddleware({
  name: "HarnessResponseFormat",
  wrapModelCall(request, handler) {
    return handler({
      ...request,
      ...currentResponseFormat == null ? {} : { responseFormat: currentResponseFormat }
    });
  }
});
function buildHostTools(toolSchemas) {
  return (toolSchemas ?? []).map(
    (schema) => tool3(
      async (input) => {
        const turn = currentTurn;
        if (!turn) throw new Error("no active turn");
        const toolCallId = `${schema.name}-${randomUUID3()}`;
        turn.emit({
          type: "tool-call",
          toolCallId,
          toolName: schema.name,
          input: JSON.stringify(input),
          providerExecuted: false
        });
        const { output, isError } = await turn.requestToolResult(toolCallId);
        turn.emit({
          type: "tool-result",
          toolCallId,
          toolName: schema.name,
          result: output ?? null,
          ...isError !== void 0 ? { isError } : {}
        });
        return typeof output === "string" ? output : JSON.stringify(output);
      },
      {
        name: schema.name,
        description: schema.description ?? "",
        schema: jsonSchemaToZodObject(schema.inputSchema)
      }
    )
  );
}
async function runTurn(start, turn) {
  currentTurn = turn;
  if (start.model) activeModel = start.model;
  activeThinking = start.thinking;
  activeEffort = start.effort;
  activeHeaders = start.headers;
  currentResponseFormat = start.responseFormat?.type === "json" && start.responseFormat.schema != null ? toolStrategy(start.responseFormat.schema) : void 0;
  const emit = (event) => turn.emit(event);
  const interruptOn = buildInterruptOn(
    start.permissionMode,
    start.builtinToolFiltering
  );
  const config = {
    version: "v2",
    configurable: { thread_id: "bridge-session" },
    ...start.recursionLimit != null ? { recursionLimit: start.recursionLimit } : {},
    signal: turn.abortSignal
  };
  const nextAgentConfigurationSignature = JSON.stringify({
    instructions: start.instructions,
    tools: start.tools,
    skillsPaths: start.skillsPaths
  });
  const rebuildAgent = agent == null || agentConfigurationSignature !== nextAgentConfigurationSignature || start.skillsChanged === true;
  if (rebuildAgent) {
    if (agent != null && start.skillsChanged === true) {
      await agent.updateState(config, {
        skillsMetadata: new Overwrite([])
      });
    }
    await closeMcpClient();
    const builtinToolFilteringMiddleware = createBuiltinToolFilteringMiddleware(
      {
        builtinToolFiltering: start.builtinToolFiltering,
        emit: (event) => {
          const turn2 = currentTurn;
          if (!turn2) throw new Error("no active turn");
          turn2.emit(event);
        }
      }
    );
    const middleware = [
      responseFormatMiddleware,
      modelMiddleware,
      ...builtinToolFilteringMiddleware ? [builtinToolFilteringMiddleware] : []
    ];
    const hostTools = buildHostTools(start.tools);
    const hostToolNames2 = new Set(hostTools.map((hostTool) => hostTool.name));
    const externalTools = await loadMcpTools({
      mcpServers: start.mcpServers
    });
    const mcpTools = externalTools.filter(
      (externalTool) => !hostToolNames2.has(externalTool.name)
    );
    mcpToolNames = new Set(mcpTools.map((mcpTool) => mcpTool.name));
    agent = createDeepAgent({
      tools: [...mcpTools, ...hostTools],
      backend: createLocalShellBackend({ rootDir: workdir }),
      systemPrompt: start.instructions ? { suffix: start.instructions } : void 0,
      // Native skills loaded from the source dirs ($HOME-materialized + <workDir> for repo-provided skills).
      ...start.skillsPaths?.length ? { skills: start.skillsPaths } : {},
      ...middleware.length > 0 ? { middleware } : {},
      // Gate built-in tools behind HITL approval when the permission mode requires it.
      ...interruptOn ? { interruptOn } : {},
      // Real instance (LangGraph rejects `true` for root graphs); gives multi-turn memory.
      checkpointer
    });
    agentConfigurationSignature = nextAgentConfigurationSignature;
  }
  const activeAgent = agent;
  if (activeAgent == null) {
    throw new Error("Deep Agents runtime was not initialized");
  }
  const hostToolNames = new Set((start.tools ?? []).map((t) => t.name));
  const streamEventState = createDeepAgentsStreamEventState();
  const emitStreamEvent = createEmitStreamEvent({
    state: streamEventState,
    configuredModel: activeModel,
    hostToolNames,
    mcpToolNames,
    structuredOutputToolNames: new Set(
      currentResponseFormat?.map((format) => format.name)
    ),
    emit
  });
  const readPendingApprovals = async () => {
    try {
      const state = await activeAgent.getState({
        configurable: { thread_id: "bridge-session" }
      });
      return collectActionRequests(
        (state.tasks ?? []).flatMap((t) => t.interrupts ?? [])
      );
    } catch {
      return [];
    }
  };
  let resumeInput = {
    messages: [{ role: "user", content: start.prompt }]
  };
  let emittedStructuredOutput = false;
  while (true) {
    const stream = await activeAgent.streamEvents(resumeInput, config);
    for await (const event of stream) {
      emitStreamEvent(event);
      const streamEvent = event;
      const namespace = streamEvent.metadata?.langgraph_checkpoint_ns ?? "";
      const output = streamEvent.data?.output;
      if (!emittedStructuredOutput && streamEvent.event === "on_chain_end" && !namespace.includes("|") && output?.structuredResponse !== void 0) {
        const id = `structured-output-${randomUUID3()}`;
        emit({ type: "text-start", id });
        emit({
          type: "text-delta",
          id,
          delta: JSON.stringify(output.structuredResponse)
        });
        emit({ type: "text-end", id });
        emittedStructuredOutput = true;
      }
    }
    const actionRequests = await readPendingApprovals();
    if (actionRequests.length === 0) break;
    const decisions = [];
    for (const action of actionRequests) {
      const approvalId = `approval-${randomUUID3()}`;
      endTextBlock({ state: streamEventState, emit });
      endReasoningBlock({ state: streamEventState, emit });
      emit({
        type: "tool-call",
        toolCallId: approvalId,
        toolName: toCommonName2(action.name),
        input: JSON.stringify(action.args ?? {}),
        providerExecuted: true,
        nativeName: action.name
      });
      emit({
        type: "tool-approval-request",
        approvalId,
        toolCallId: approvalId
      });
      flushStep({ state: streamEventState, emit });
      const decision = await turn.requestToolApproval(approvalId);
      if (decision.approved) {
        const queue = streamEventState.approvedToolQueue.get(action.name) ?? [];
        queue.push(approvalId);
        streamEventState.approvedToolQueue.set(action.name, queue);
        decisions.push({ type: "approve" });
      } else {
        emit({
          type: "tool-result",
          toolCallId: approvalId,
          toolName: toCommonName2(action.name),
          result: decision.reason ?? "Rejected by user."
        });
        decisions.push({
          type: "reject",
          ...decision.reason ? { message: decision.reason } : {}
        });
      }
    }
    resumeInput = new Command5({ resume: { decisions } });
  }
  endTextBlock({ state: streamEventState, emit });
  endReasoningBlock({ state: streamEventState, emit });
  flushStep({ state: streamEventState, emit });
  emit({
    type: "finish",
    finishReason: { unified: "stop" },
    totalUsage: {
      inputTokens: { total: streamEventState.inputTokens },
      outputTokens: { total: streamEventState.outputTokens }
    }
  });
}
await runBridge({
  bridgeType: "deepagents",
  bridgeStateDir,
  onStart: runTurn,
  onStop: async () => {
    await closeMcpClient();
    await saveMemorySaver({
      path: conversationCheckpointPath,
      saver: checkpointer
    });
    return {};
  },
  onDestroy: async () => {
    await closeMcpClient();
    await removeMemorySaverSnapshot(conversationCheckpointPath);
  }
});
async function loadMcpTools({
  mcpServers
}) {
  if (mcpServers == null || Object.keys(mcpServers).length === 0) return [];
  for (const [name, value] of Object.entries(mcpServers)) {
    if (value == null || typeof value !== "object" || Array.isArray(value)) {
      throw new Error(
        `DeepAgents MCP server ${JSON.stringify(name)} must be configured with an object value.`
      );
    }
  }
  mcpClient = new MultiServerMCPClient({
    mcpServers,
    prefixToolNameWithServerName: true,
    additionalToolNamePrefix: "mcp"
  });
  return mcpClient.getTools();
}
async function closeMcpClient() {
  const client = mcpClient;
  mcpClient = void 0;
  mcpToolNames = /* @__PURE__ */ new Set();
  await client?.close();
}
//# sourceMappingURL=index.mjs.map