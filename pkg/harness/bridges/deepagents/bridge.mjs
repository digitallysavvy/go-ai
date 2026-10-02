import { randomUUID } from "node:crypto";
import { argv, env, pid, stdout } from "node:process";
import { appendFile, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { existsSync, readFileSync } from "node:fs";
import { WebSocketServer } from "ws";
import { ChatAnthropic } from "@langchain/anthropic";
import { tool } from "@langchain/core/tools";
import { Command, MemorySaver, Overwrite } from "@langchain/langgraph";
import { MultiServerMCPClient } from "@langchain/mcp-adapters";
import { LocalShellBackend, createDeepAgent } from "deepagents";
import { createMiddleware, toolStrategy } from "langchain";
import { z } from "zod/v4";
import { dirname } from "node:path";
import { AIMessage, ToolMessage } from "@langchain/core/messages";
//#region ../harness/dist/bridge/index.js
const DEBUG_LEVEL_WEIGHT = {
	error: 0,
	warn: 1,
	info: 2,
	debug: 3,
	trace: 4
};
/** Exact-or-dotted-prefix subsystem match (`'bridge'` matches `'bridge.turn'`). */
function subsystemMatches(filters, subsystem) {
	if (!filters || filters.length === 0) return true;
	return filters.some((filter) => subsystem === filter || subsystem.startsWith(`${filter}.`));
}
function formatBridgeError(err) {
	if (err instanceof Error) return {
		name: err.name,
		message: err.message,
		stack: err.stack
	};
	if (typeof err === "string") return { message: err };
	if (err !== null && typeof err === "object") try {
		return { message: JSON.stringify(err) };
	} catch {}
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
			if (existing.response != null) options.respond(existing.response);
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
		entries.set(input.messageId, { reject: message.reject });
		pendingCount++;
		if (closed) {
			message.reject(/* @__PURE__ */ new Error("The bridge turn is no longer accepting user messages."));
			return;
		}
		const waiter = waiters.shift();
		if (waiter != null) waiter({
			done: false,
			value: message
		});
		else messages.push(message);
	};
	const close = (error) => {
		if (closed) return;
		closed = true;
		const reason = error ?? /* @__PURE__ */ new Error("The bridge turn ended before accepting the user message.");
		for (const entry of entries.values()) if (entry.response == null) entry.reject(reason);
		messages.length = 0;
		while (waiters.length > 0) waiters.shift()({
			done: true,
			value: void 0
		});
	};
	return {
		get pendingCount() {
			return pendingCount;
		},
		enqueue,
		close,
		[Symbol.asyncIterator]() {
			return { next: () => {
				const message = messages.shift();
				if (message != null) return Promise.resolve({
					done: false,
					value: message
				});
				if (closed) return Promise.resolve({
					done: true,
					value: void 0
				});
				return new Promise((resolve) => {
					waiters.push(resolve);
				});
			} };
		}
	};
}
function parseEnvList(value) {
	if (!value) return void 0;
	const items = value.split(",").map((item) => item.trim()).filter(Boolean);
	return items.length > 0 ? items : void 0;
}
const ENV_TRUTHY = /* @__PURE__ */ new Set([
	"1",
	"true",
	"yes",
	"on"
]);
const WS_OPEN = 1;
async function runBridge(options) {
	const { bridgeType, bridgeStateDir, onStart, onStop, onDestroy } = options;
	const teardownGraceMs = options.turnTeardownGraceMs ?? 1e4;
	const expectedToken = options.token ?? env.BRIDGE_CHANNEL_TOKEN ?? "";
	const bridgeWsPort = options.port ?? parseInt(env.BRIDGE_WS_PORT ?? "0", 10);
	const bridgeMetaPath = `${bridgeStateDir}/bridge-meta.json`;
	const startConfigPath = `${bridgeStateDir}/start-config.json`;
	const rerunStartConfigPath = `${bridgeStateDir}/rerun-start-config.json`;
	const eventLogPath = `${bridgeStateDir}/event-log.ndjson`;
	try {
		await mkdir(bridgeStateDir, { recursive: true });
	} catch {}
	let currentBoundPort = 0;
	let currentTurnState = "init";
	let activeSocket;
	let isFirstTurn = true;
	let turnAbort;
	let currentUserMessages;
	/**
	* Settles when the in-flight turn has fully wound down — `onStart`
	* returned or threw AND its completion state was recorded. `undefined`
	* between turns. A new `start` fences on this so turns never overlap.
	*/
	let activeTurn;
	let debugConfig;
	let consoleCaptureInstalled = false;
	const envDebugEnabled = ENV_TRUTHY.has((env.HARNESS_DEBUG ?? "").toLowerCase());
	let seqCounter = 0;
	let eventLog = [];
	let diskBuffer = "";
	let flushPromise = null;
	const flushEventsToDisk = async () => {
		while (diskBuffer.length > 0) {
			const buf = diskBuffer;
			diskBuffer = "";
			await appendFile(eventLogPath, buf).catch(() => {});
		}
	};
	const scheduleEventFlush = () => {
		if (flushPromise) return;
		flushPromise = new Promise((resolve) => {
			setImmediate(() => {
				flushEventsToDisk().finally(resolve);
			});
		}).finally(() => {
			flushPromise = null;
			if (diskBuffer.length > 0) scheduleEventFlush();
		});
	};
	const flushPendingEventsToDisk = async () => {
		if (diskBuffer.length > 0 && !flushPromise) scheduleEventFlush();
		let inFlight = flushPromise;
		while (inFlight) {
			await inFlight;
			inFlight = flushPromise;
		}
	};
	if (env.BRIDGE_REPLAY_FROM_DISK === "1" && existsSync(eventLogPath)) try {
		eventLog = readFileSync(eventLogPath, "utf8").split("\n").map((line) => line.trim()).filter(Boolean).map((line) => ({
			seq: JSON.parse(line).seq,
			line
		}));
		seqCounter = eventLog.at(-1)?.seq ?? 0;
	} catch {
		eventLog = [];
		seqCounter = 0;
	}
	const pendingToolResults = /* @__PURE__ */ new Map();
	const bufferedToolResults = [];
	const pendingToolApprovals = /* @__PURE__ */ new Map();
	const writeBridgeMeta = async (state) => {
		try {
			await writeFile(bridgeMetaPath, JSON.stringify({
				type: bridgeType,
				port: currentBoundPort,
				state,
				pid
			}));
		} catch {}
	};
	const writeStartConfig = async (start) => {
		try {
			const serialized = JSON.stringify(start);
			await writeFile(startConfigPath, serialized);
			if (!existsSync(rerunStartConfigPath)) await writeFile(rerunStartConfigPath, serialized);
		} catch {}
	};
	const emit = (event) => {
		const seq = ++seqCounter;
		const line = JSON.stringify({
			...event,
			seq
		});
		eventLog.push({
			seq,
			line
		});
		diskBuffer += `${line}\n`;
		scheduleEventFlush();
		if (activeSocket?.readyState === WS_OPEN) try {
			activeSocket.send(line);
		} catch {}
	};
	const replay = (ws, afterSeq) => {
		for (const entry of eventLog) if (entry.seq > afterSeq && ws.readyState === WS_OPEN) ws.send(entry.line);
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
			rawStderrWrite(`[harness:${bridgeType}:error] ${input.message}: ${formatted.message}\n`);
			if (formatted.stack) rawStderrWrite(`${formatted.stack}\n`);
		} catch {}
	};
	const emitWarning = (input) => {
		try {
			for (const line of input.message.split("\n")) if (line.trim().length > 0) rawStderrWrite(`[harness:${bridgeType}:warn] ${line}\n`);
		} catch {}
	};
	const emitError = (input) => {
		writeErrorToStderr({
			message: input.message ?? "bridge error",
			error: input.error
		});
		emit({
			type: "error",
			error: serialiseError(input.error)
		});
	};
	const installConsoleCapture = () => {
		if (consoleCaptureInstalled) return;
		consoleCaptureInstalled = true;
		const buffers = {
			stdout: "",
			stderr: ""
		};
		const patch = (stream, raw) => (chunk, encoding, cb) => {
			if (debugConfig?.enabled) try {
				const enc = typeof encoding === "string" ? encoding : "utf8";
				const text = typeof chunk === "string" ? chunk : Buffer.from(chunk).toString(enc);
				const parts = (buffers[stream] + text.replace(/\r\n/g, "\n")).split("\n");
				buffers[stream] = parts.pop() ?? "";
				for (const line of parts) {
					const trimmed = line.replace(/\s+$/, "");
					if (trimmed) emit({
						type: "sandbox-log",
						source: bridgeType,
						stream,
						line: trimmed
					});
				}
			} catch {}
			return raw(chunk, encoding, cb);
		};
		process.stdout.write = patch("stdout", rawStdoutWrite);
		process.stderr.write = patch("stderr", rawStderrWrite);
	};
	const handleInbound = async (msg, ws) => {
		switch (msg.type) {
			case "start": {
				for (;;) {
					const pendingTurn = activeTurn;
					if (pendingTurn == null) break;
					turnAbort?.abort();
					currentUserMessages?.close(/* @__PURE__ */ new Error("A new bridge turn replaced the active turn."));
					let graceTimer;
					const settled = await Promise.race([pendingTurn.then(() => true), new Promise((resolve) => {
						graceTimer = setTimeout(() => resolve(false), teardownGraceMs);
						graceTimer.unref?.();
					})]);
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
				writeFile(eventLogPath, "").catch(() => {});
				turnAbort = new AbortController();
				currentTurnState = "running";
				writeStartConfig(msg);
				writeBridgeMeta("running");
				const startDebug = msg.debug;
				debugConfig = {
					enabled: startDebug?.enabled ?? envDebugEnabled,
					level: startDebug?.level ?? env.HARNESS_DEBUG_LEVEL,
					subsystems: startDebug?.subsystems ?? parseEnvList(env.HARNESS_DEBUG_SUBSYSTEMS)
				};
				if (debugConfig.enabled) installConsoleCapture();
				const userMessages = createBridgeUserMessageQueue({ respond: emit });
				const turn = {
					emit,
					requestToolResult: (requestInput) => {
						const request = typeof requestInput === "string" ? { toolCallId: requestInput } : requestInput;
						const bufferedIndex = bufferedToolResults.findIndex((buffered) => buffered.toolCallId === request.toolCallId || request.matches?.(buffered.result) === true);
						if (bufferedIndex >= 0) return Promise.resolve(bufferedToolResults.splice(bufferedIndex, 1)[0].result);
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
					emitError({
						error: err,
						message: "bridge turn failed"
					});
				} finally {
					userMessages.close();
					if (currentUserMessages === userMessages) currentUserMessages = void 0;
					if (activeTurn === thisTurn) {
						activeTurn = void 0;
						currentTurnState = "waiting";
						writeBridgeMeta("waiting");
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
				const matchingPending = exactPending == null ? Array.from(pendingToolResults.entries()).find(([, pending]) => pending.matches?.(result) === true) : void 0;
				const pending = exactPending ?? matchingPending?.[1];
				const pendingId = exactPending != null ? msg.toolCallId : matchingPending?.[0];
				if (pending != null && pendingId != null) {
					pendingToolResults.delete(pendingId);
					pending.resolve(result);
				} else bufferedToolResults.push({
					toolCallId: msg.toolCallId,
					result
				});
				return;
			}
			case "tool-approval-response": {
				const resolver = pendingToolApprovals.get(msg.approvalId);
				if (resolver) {
					pendingToolApprovals.delete(msg.approvalId);
					resolver({
						approved: msg.approved,
						reason: msg.reason
					});
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
						error: { message: "The connection does not own the active bridge turn." }
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
				writeBridgeMeta("done");
				await onDestroy?.();
				drainThenExit(ws, 1e3, "destroy");
				return;
			case "stop":
				currentTurnState = "done";
				writeBridgeMeta("done");
				sendControl(ws, {
					type: "bridge-stop",
					data: await onStop?.() ?? {}
				});
				drainThenExit(ws, 1e3, "stop");
		}
	};
	writeBridgeMeta("init");
	const wss = new WebSocketServer({
		port: bridgeWsPort,
		host: "0.0.0.0"
	});
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
			if (ws.bufferedAmount === 0 || ws.readyState !== WS_OPEN || Date.now() - start >= 5e3) {
				flushPendingEventsToDisk().finally(() => {
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
		writeBridgeMeta("waiting");
		stdout.write(JSON.stringify({
			type: "bridge-ready",
			port: currentBoundPort
		}) + "\n");
		options.onListening?.(currentBoundPort);
	});
	wss.on("connection", (ws, req) => {
		if (new URL(req.url ?? "/", "http://localhost").searchParams.get("agent_bridge_token") !== expectedToken) {
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
			handleInbound(parsed, ws);
		});
		ws.on("close", () => {
			if (activeSocket === ws) activeSocket = void 0;
		});
		ws.on("error", () => {});
	});
	process.on("uncaughtException", (err) => {
		emitError({
			error: err,
			message: "uncaught exception"
		});
	});
	process.on("unhandledRejection", (err) => {
		emitError({
			error: err,
			message: "unhandled rejection"
		});
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
	if (socket?.readyState === WS_OPEN) try {
		socket.send(JSON.stringify(message));
	} catch {}
}
function serialiseError(err) {
	if (err instanceof Error) return {
		name: err.name,
		message: err.message,
		stack: err.stack
	};
	return err;
}
//#endregion
//#region src/bridge/approvals.ts
const NATIVE_TOOL_KIND = {
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
const NATIVE_TO_COMMON$1 = {
	read_file: "read",
	write_file: "write",
	edit_file: "edit",
	execute: "bash"
};
function toCommonName$1(nativeName) {
	return NATIVE_TO_COMMON$1[nativeName] ?? nativeName;
}
function isBuiltinToolIncluded(input) {
	if (input.toolFiltering == null) return true;
	const toolName = toCommonName$1(input.nativeName);
	return input.toolFiltering.mode === "allow" ? input.toolFiltering.toolNames.includes(toolName) : !input.toolFiltering.toolNames.includes(toolName);
}
function builtinToolRequiresApproval(kind, permissionMode) {
	if (permissionMode === "allow-all") return false;
	if (permissionMode === "allow-edits") return kind === "bash";
	return kind === "edit" || kind === "bash";
}
function buildInterruptOn(permissionMode, builtinToolFiltering) {
	const config = {};
	for (const [nativeName, kind] of Object.entries(NATIVE_TOOL_KIND)) if (permissionMode != null && isBuiltinToolIncluded({
		nativeName,
		toolFiltering: builtinToolFiltering
	}) && builtinToolRequiresApproval(kind, permissionMode)) config[nativeName] = { allowedDecisions: ["approve", "reject"] };
	return Object.keys(config).length > 0 ? config : void 0;
}
function collectActionRequests(interrupts) {
	const out = [];
	for (const interrupt of interrupts) {
		const value = interrupt.value;
		for (const action of value?.actionRequests ?? []) out.push({
			name: action.name,
			args: action.args ?? {}
		});
	}
	return out;
}
//#endregion
//#region src/bridge/create-emit-stream-event.ts
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
const NATIVE_TO_COMMON = {
	read_file: "read",
	write_file: "write",
	edit_file: "edit",
	execute: "bash"
};
function toCommonName(nativeName) {
	return NATIVE_TO_COMMON[nativeName] ?? nativeName;
}
function createEmitStreamEvent({ state, configuredModel, hostToolNames, mcpToolNames, structuredOutputToolNames = /* @__PURE__ */ new Set(), emit }) {
	return (event) => {
		const kind = event.event;
		const data = event.data ?? {};
		const nested = (event.metadata?.langgraph_checkpoint_ns ?? "").includes("|");
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
				flushStep({
					state,
					emit
				});
			}
		} else if (kind === "on_chat_model_stream") {
			if (nested) return;
			const chunk = data.chunk;
			if (!chunk) return;
			const content = chunk.content;
			if (typeof content === "string" && content) emitText({
				state,
				emit,
				delta: content
			});
			else if (Array.isArray(content)) {
				for (const block of content) if (block && typeof block === "object") {
					const value = block;
					if ((value.type === "text" || value.type === "text-delta") && value.text) emitText({
						state,
						emit,
						delta: value.text
					});
					else if (value.type === "thinking" && value.thinking) emitReasoning({
						state,
						emit,
						delta: value.thinking
					});
					else if ((value.type === "reasoning" || value.type === "reasoning-delta") && value.reasoning) emitReasoning({
						state,
						emit,
						delta: value.reasoning
					});
				}
			}
			const usage = chunk.usage_metadata;
			if (usage) {
				state.streamedStepInput = Math.max(state.streamedStepInput, usage.input_tokens ?? 0);
				state.streamedStepOutput = Math.max(state.streamedStepOutput, usage.output_tokens ?? 0);
			}
		} else if (kind === "on_chat_model_end") {
			const usage = data.output?.usage_metadata;
			const stepInput = usage?.input_tokens ?? state.streamedStepInput;
			const stepOutput = usage?.output_tokens ?? state.streamedStepOutput;
			state.inputTokens += stepInput;
			state.outputTokens += stepOutput;
			state.streamedStepInput = 0;
			state.streamedStepOutput = 0;
			if (!nested) {
				endTextBlock({
					state,
					emit
				});
				endReasoningBlock({
					state,
					emit
				});
				state.pendingStep = {
					input: stepInput,
					output: stepOutput
				};
			}
		} else if (kind === "on_tool_start") {
			const toolName = event.name ?? "unknown";
			if (structuredOutputToolNames.has(toolName)) return;
			const runId = event.run_id ?? "";
			if (!nested && !hostToolNames.has(toolName)) {
				const isMcpTool = mcpToolNames.has(toolName);
				if (isMcpTool && runId) state.dynamicToolRunIds.add(runId);
				const queued = state.approvedToolQueue.get(toolName);
				if (queued && queued.length > 0) {
					const approvalId = queued.shift();
					if (runId) state.approvedRunIds.set(runId, approvalId);
				} else {
					endTextBlock({
						state,
						emit
					});
					endReasoningBlock({
						state,
						emit
					});
					emit({
						type: "tool-call",
						toolCallId: runId,
						toolName: toCommonName(toolName),
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
				if (output && typeof output === "object" && "content" in output) output = output.content;
				emit({
					type: "tool-result",
					toolCallId: state.approvedRunIds.get(runId) ?? runId,
					toolName: toCommonName(toolName),
					result: output ?? null,
					...dynamic ? { dynamic: true } : {}
				});
				state.approvedRunIds.delete(runId);
			}
		}
	};
}
function endTextBlock({ state, emit }) {
	if (state.textBlockId) {
		emit({
			type: "text-end",
			id: state.textBlockId
		});
		state.textBlockId = void 0;
	}
}
function endReasoningBlock({ state, emit }) {
	if (state.reasoningBlockId) {
		emit({
			type: "reasoning-end",
			id: state.reasoningBlockId
		});
		state.reasoningBlockId = void 0;
	}
}
function flushStep({ state, emit }) {
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
function ensureTextBlock({ state, emit }) {
	if (!state.textBlockId) {
		state.textBlockId = `text-${randomUUID()}`;
		emit({
			type: "text-start",
			id: state.textBlockId
		});
	}
	return state.textBlockId;
}
function emitText({ state, emit, delta }) {
	endReasoningBlock({
		state,
		emit
	});
	emit({
		type: "text-delta",
		id: ensureTextBlock({
			state,
			emit
		}),
		delta
	});
}
function emitReasoning({ state, emit, delta }) {
	endTextBlock({
		state,
		emit
	});
	if (!state.reasoningBlockId) {
		state.reasoningBlockId = `reasoning-${randomUUID()}`;
		emit({
			type: "reasoning-start",
			id: state.reasoningBlockId
		});
	}
	emit({
		type: "reasoning-delta",
		id: state.reasoningBlockId,
		delta
	});
}
function resolveDeepAgentsModelId({ configuredModel, metadata }) {
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
//#endregion
//#region src/bridge/json-schema-to-zod.ts
function jsonSchemaToZodObject(input) {
	const schema = input && typeof input === "object" ? input : {};
	return z.object(toZodShape(schema));
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
	if (!schema) return z.any();
	const types = Array.isArray(schema.type) ? schema.type.filter((t) => t !== "null") : [schema.type].filter(Boolean);
	let zType;
	switch (types[0]) {
		case "string":
			zType = z.string();
			break;
		case "number":
			zType = z.number();
			break;
		case "integer":
			zType = z.number().int();
			break;
		case "boolean":
			zType = z.boolean();
			break;
		case "array":
			zType = z.array(toZodType(schema.items));
			break;
		case "object":
			zType = z.object(toZodShape(schema));
			break;
		case "null":
			zType = z.null();
			break;
		default: zType = z.any();
	}
	if (schema.description) zType = zType.describe(schema.description);
	if (schema.nullable) zType = zType.nullable();
	return zType;
}
function createLocalShellBackend({ rootDir, env = process.env }) {
	return new LocalShellBackend({
		rootDir,
		env: { PATH: env.PATH ?? "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" }
	});
}
//#endregion
//#region src/bridge/persistent-memory-saver.ts
const SNAPSHOT_HEADER = "deepagents-memory-saver-v1";
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
async function loadMemorySaver({ path, saver }) {
	let snapshot;
	try {
		snapshot = await readFile(path, "utf8");
	} catch (error) {
		if (error.code === "ENOENT") return;
		throw error;
	}
	const [header, ...lines] = snapshot.split("\n");
	if (header !== SNAPSHOT_HEADER) throw new Error("Unsupported Deep Agents conversation checkpoint format");
	const storage = Object.create(null);
	const writes = Object.create(null);
	for (const line of lines) {
		if (line === "") continue;
		const fields = line.split("	");
		if (fields[0] === "S" && fields.length === 7) {
			const [, threadIdValue, namespaceValue, checkpointIdValue] = fields;
			const threadId = decodeString(threadIdValue);
			const namespace = decodeString(namespaceValue);
			const checkpointId = decodeString(checkpointIdValue);
			storage[threadId] ??= Object.create(null);
			storage[threadId][namespace] ??= Object.create(null);
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
			writes[key] ??= Object.create(null);
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
async function saveMemorySaver({ path, saver }) {
	const lines = [SNAPSHOT_HEADER];
	for (const [threadId, namespaces] of Object.entries(saver.storage)) for (const [namespace, checkpoints] of Object.entries(namespaces)) for (const [checkpointId, [checkpoint, metadata, parentCheckpointId]] of Object.entries(checkpoints)) lines.push([
		"S",
		encodeString(threadId),
		encodeString(namespace),
		encodeString(checkpointId),
		encodeBytes(checkpoint),
		encodeBytes(metadata),
		parentCheckpointId == null ? "" : encodeString(parentCheckpointId)
	].join("	"));
	for (const [key, indexedWrites] of Object.entries(saver.writes)) for (const [index, [taskId, channel, value]] of Object.entries(indexedWrites)) lines.push([
		"W",
		encodeString(key),
		encodeString(index),
		encodeString(taskId),
		encodeString(channel),
		encodeBytes(value)
	].join("	"));
	await mkdir(dirname(path), { recursive: true });
	const temporaryPath = `${path}.${process.pid}.tmp`;
	await writeFile(temporaryPath, `${lines.join("\n")}\n`, "utf8");
	await rename(temporaryPath, path);
}
async function removeMemorySaverSnapshot(path) {
	await rm(path, { force: true });
}
//#endregion
//#region src/bridge/tool-filtering.ts
const MIDDLEWARE_BRAND = Symbol.for("AgentMiddleware");
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
		[MIDDLEWARE_BRAND]: true,
		name: "HarnessBuiltinToolFilteringMiddleware",
		afterModel: {
			canJumpTo: ["model"],
			hook: (state) => {
				const lastMessage = [...state.messages].reverse().find((message) => AIMessage.isInstance(message));
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
					const toolName = toCommonName$1(nativeName);
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
					deniedToolMessages.push(new ToolMessage({
						content: reason,
						name: nativeName,
						tool_call_id: toolCall.id,
						status: "error"
					}));
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
//#endregion
//#region src/bridge/index.ts
const HARNESS_CLIENT_APP = env.AI_SDK_HARNESS_CLIENT_APP;
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
function buildModel({ rawModel, thinking, effort, headers }) {
	if (!rawModel) return void 0;
	const baseUrl = env.ANTHROPIC_BASE_URL;
	const model = baseUrl ? rawModel : rawModel.replace(/^anthropic[/:]/, "");
	return new ChatAnthropic({
		model,
		...thinking ? { thinking } : {},
		...effort ? { outputConfig: { effort } } : {},
		...env.ANTHROPIC_API_KEY ? { apiKey: env.ANTHROPIC_API_KEY } : {},
		...baseUrl ? { anthropicApiUrl: baseUrl } : {},
		...headers != null || env.AI_GATEWAY_API_KEY ? { clientOptions: { defaultHeaders: {
			...headers,
			...env.AI_GATEWAY_API_KEY && HARNESS_CLIENT_APP ? {
				"User-Agent": HARNESS_CLIENT_APP,
				"x-client-app": HARNESS_CLIENT_APP
			} : {}
		} } } : {}
	});
}
function createModelMiddleware() {
	return createMiddleware({
		name: "harnessModel",
		wrapModelCall: async (request, handler) => {
			if (!activeModel && !activeThinking && !activeEffort && !activeHeaders) return handler(request);
			if (activeModel) {
				const configuredModel = buildModel({
					rawModel: activeModel,
					thinking: activeThinking,
					effort: activeEffort,
					headers: activeHeaders
				});
				if (!configuredModel) throw new Error("Deep Agents model is missing");
				return handler({
					...request,
					model: configuredModel
				});
			}
			let model = request.model;
			if ("_getModelInstance" in model && typeof model._getModelInstance === "function") model = await model._getModelInstance();
			if (!(model instanceof ChatAnthropic)) throw new Error("Deep Agents reasoning requires ChatAnthropic");
			const configuredModel = buildModel({
				rawModel: model.model,
				thinking: activeThinking,
				effort: activeEffort,
				headers: activeHeaders
			});
			if (!configuredModel) throw new Error("Deep Agents model is missing");
			return handler({
				...request,
				model: configuredModel
			});
		}
	});
}
const args = parseArgs(argv.slice(2));
const workdir = args.workdir;
const bridgeStateDir = args.bridgeStateDir;
if (!workdir || !bridgeStateDir) {
	console.error("deepagents bridge: missing --workdir / --bridge-state-dir");
	process.exit(1);
}
const conversationCheckpointPath = `${bridgeStateDir}/conversation.checkpoint`;
let agent;
let currentTurn;
let mcpClient;
let mcpToolNames = /* @__PURE__ */ new Set();
let currentResponseFormat;
const checkpointer = new MemorySaver();
if (args.resume === "true") await loadMemorySaver({
	path: conversationCheckpointPath,
	saver: checkpointer
});
else await removeMemorySaverSnapshot(conversationCheckpointPath);
let agentConfigurationSignature;
let activeModel;
let activeThinking;
let activeEffort;
let activeHeaders;
const modelMiddleware = createModelMiddleware();
const responseFormatMiddleware = createMiddleware({
	name: "HarnessResponseFormat",
	wrapModelCall(request, handler) {
		return handler({
			...request,
			...currentResponseFormat == null ? {} : { responseFormat: currentResponseFormat }
		});
	}
});
function buildHostTools(toolSchemas) {
	return (toolSchemas ?? []).map((schema) => tool(async (input) => {
		const turn = currentTurn;
		if (!turn) throw new Error("no active turn");
		const toolCallId = `${schema.name}-${randomUUID()}`;
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
	}, {
		name: schema.name,
		description: schema.description ?? "",
		schema: jsonSchemaToZodObject(schema.inputSchema)
	}));
}
async function runTurn(start, turn) {
	currentTurn = turn;
	if (start.model) activeModel = start.model;
	activeThinking = start.thinking;
	activeEffort = start.effort;
	activeHeaders = start.headers;
	currentResponseFormat = start.responseFormat?.type === "json" && start.responseFormat.schema != null ? toolStrategy(start.responseFormat.schema) : void 0;
	const emit = (event) => turn.emit(event);
	const interruptOn = buildInterruptOn(start.permissionMode, start.builtinToolFiltering);
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
	if (agent == null || agentConfigurationSignature !== nextAgentConfigurationSignature || start.skillsChanged === true) {
		if (agent != null && start.skillsChanged === true) await agent.updateState(config, { skillsMetadata: new Overwrite([]) });
		await closeMcpClient();
		const builtinToolFilteringMiddleware = createBuiltinToolFilteringMiddleware({
			builtinToolFiltering: start.builtinToolFiltering,
			emit: (event) => {
				const turn = currentTurn;
				if (!turn) throw new Error("no active turn");
				turn.emit(event);
			}
		});
		const middleware = [
			responseFormatMiddleware,
			modelMiddleware,
			...builtinToolFilteringMiddleware ? [builtinToolFilteringMiddleware] : []
		];
		const hostTools = buildHostTools(start.tools);
		const hostToolNames = new Set(hostTools.map((hostTool) => hostTool.name));
		const mcpTools = (await loadMcpTools({ mcpServers: start.mcpServers })).filter((externalTool) => !hostToolNames.has(externalTool.name));
		mcpToolNames = new Set(mcpTools.map((mcpTool) => mcpTool.name));
		agent = createDeepAgent({
			tools: [...mcpTools, ...hostTools],
			backend: createLocalShellBackend({ rootDir: workdir }),
			systemPrompt: start.instructions ? { suffix: start.instructions } : void 0,
			...start.skillsPaths?.length ? { skills: start.skillsPaths } : {},
			...middleware.length > 0 ? { middleware } : {},
			...interruptOn ? { interruptOn } : {},
			checkpointer
		});
		agentConfigurationSignature = nextAgentConfigurationSignature;
	}
	const activeAgent = agent;
	if (activeAgent == null) throw new Error("Deep Agents runtime was not initialized");
	const hostToolNames = new Set((start.tools ?? []).map((t) => t.name));
	const streamEventState = createDeepAgentsStreamEventState();
	const emitStreamEvent = createEmitStreamEvent({
		state: streamEventState,
		configuredModel: activeModel,
		hostToolNames,
		mcpToolNames,
		structuredOutputToolNames: new Set(currentResponseFormat?.map((format) => format.name)),
		emit
	});
	const readPendingApprovals = async () => {
		try {
			return collectActionRequests(((await activeAgent.getState({ configurable: { thread_id: "bridge-session" } })).tasks ?? []).flatMap((t) => t.interrupts ?? []));
		} catch {
			return [];
		}
	};
	let resumeInput = { messages: [{
		role: "user",
		content: start.prompt
	}] };
	let emittedStructuredOutput = false;
	while (true) {
		const stream = await activeAgent.streamEvents(resumeInput, config);
		for await (const event of stream) {
			emitStreamEvent(event);
			const streamEvent = event;
			const namespace = streamEvent.metadata?.langgraph_checkpoint_ns ?? "";
			const output = streamEvent.data?.output;
			if (!emittedStructuredOutput && streamEvent.event === "on_chain_end" && !namespace.includes("|") && output?.structuredResponse !== void 0) {
				const id = `structured-output-${randomUUID()}`;
				emit({
					type: "text-start",
					id
				});
				emit({
					type: "text-delta",
					id,
					delta: JSON.stringify(output.structuredResponse)
				});
				emit({
					type: "text-end",
					id
				});
				emittedStructuredOutput = true;
			}
		}
		const actionRequests = await readPendingApprovals();
		if (actionRequests.length === 0) break;
		const decisions = [];
		for (const action of actionRequests) {
			const approvalId = `approval-${randomUUID()}`;
			endTextBlock({
				state: streamEventState,
				emit
			});
			endReasoningBlock({
				state: streamEventState,
				emit
			});
			emit({
				type: "tool-call",
				toolCallId: approvalId,
				toolName: toCommonName(action.name),
				input: JSON.stringify(action.args ?? {}),
				providerExecuted: true,
				nativeName: action.name
			});
			emit({
				type: "tool-approval-request",
				approvalId,
				toolCallId: approvalId
			});
			flushStep({
				state: streamEventState,
				emit
			});
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
					toolName: toCommonName(action.name),
					result: decision.reason ?? "Rejected by user."
				});
				decisions.push({
					type: "reject",
					...decision.reason ? { message: decision.reason } : {}
				});
			}
		}
		resumeInput = new Command({ resume: { decisions } });
	}
	endTextBlock({
		state: streamEventState,
		emit
	});
	endReasoningBlock({
		state: streamEventState,
		emit
	});
	flushStep({
		state: streamEventState,
		emit
	});
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
async function loadMcpTools({ mcpServers }) {
	if (mcpServers == null || Object.keys(mcpServers).length === 0) return [];
	for (const [name, value] of Object.entries(mcpServers)) if (value == null || typeof value !== "object" || Array.isArray(value)) throw new Error(`DeepAgents MCP server ${JSON.stringify(name)} must be configured with an object value.`);
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
//#endregion
export {};

//# sourceMappingURL=index.mjs.map