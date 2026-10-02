import { appendFile, mkdir, writeFile } from "node:fs/promises";
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { randomBytes, randomUUID } from "node:crypto";
import { argv, env, pid, stdout } from "node:process";
import { WebSocketServer } from "ws";
import path from "node:path";
import { isDeepStrictEqual } from "node:util";
import { createOpencodeClient, createOpencodeServer } from "@opencode-ai/sdk/v2";
import { createServer } from "node:http";
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
//#region src/bridge/opencode-types.ts
function asOpenCodeObject(value) {
	return value != null && typeof value === "object" && !Array.isArray(value) ? value : void 0;
}
//#endregion
//#region src/bridge/opencode-events.ts
function createTranslationState() {
	return {
		streamStarted: false,
		textDeltas: /* @__PURE__ */ new Map(),
		reasoningDeltas: /* @__PURE__ */ new Map(),
		toolInputs: /* @__PURE__ */ new Map(),
		toolNames: /* @__PURE__ */ new Map(),
		toolCallsEmitted: /* @__PURE__ */ new Set(),
		toolResultsEmitted: /* @__PURE__ */ new Set(),
		hostToolCallsAuthorized: /* @__PURE__ */ new Set(),
		shellCommands: /* @__PURE__ */ new Map(),
		messageRoles: /* @__PURE__ */ new Map(),
		turnUsage: void 0,
		legacyTextPartIds: /* @__PURE__ */ new Set(),
		legacyReasoningPartIds: /* @__PURE__ */ new Set(),
		legacyStepFinishPartIds: /* @__PURE__ */ new Set(),
		dynamicToolCallIds: /* @__PURE__ */ new Set()
	};
}
function emitOpenCodeStreamStart({ info, state, emit }) {
	if (state.streamStarted) return;
	const message = openCodeMessageInfoFromValue(info);
	if (!message) return;
	if (message.role !== "assistant" && message.type !== "assistant") return;
	const providerID = stringValue$1(message.providerID);
	const modelID = stringValue$1(message.modelID);
	const modelId = providerID && modelID ? `${providerID}/${modelID}` : void 0;
	state.streamStarted = true;
	emit({
		type: "stream-start",
		...modelId ? { modelId } : {}
	});
}
function unwrapOpenCodeEvent(rawEvent) {
	const raw = asOpenCodeObject(rawEvent);
	if (!raw) return void 0;
	if (raw.type === "sync" && raw.syncEvent) {
		const sync = asOpenCodeObject(raw.syncEvent);
		if (!sync) return void 0;
		return {
			id: String(sync.id ?? raw.id ?? ""),
			type: stripSyncVersion(String(sync.type ?? "")),
			properties: openCodeEventPropertiesFromValue(sync.data) ?? {}
		};
	}
	return {
		id: typeof raw.id === "string" ? raw.id : void 0,
		type: typeof raw.type === "string" ? stripSyncVersion(raw.type) : void 0,
		properties: openCodeEventPropertiesFromValue(raw.properties) ?? openCodeEventPropertiesFromValue(raw.data) ?? {}
	};
}
function getOpenCodeEventSessionId(event) {
	const props = event.properties;
	if (!props) return void 0;
	if (typeof props.sessionID === "string") return props.sessionID;
	if (typeof props.sessionId === "string") return props.sessionId;
	if (event.type?.startsWith("session.") && typeof props.id === "string") return props.id;
	const info = asOpenCodeObject(props.info);
	if (typeof info?.sessionID === "string") return info.sessionID;
	if ((event.type === "session.created" || event.type === "session.updated" || event.type === "session.deleted") && typeof info?.id === "string") return info.id;
	const part = props.part;
	const partObject = asOpenCodeObject(part);
	if (typeof partObject?.sessionID === "string") return partObject.sessionID;
}
function emitMissingFinalDelta({ id, fullText, emittedText, emit, type }) {
	if (!fullText || fullText === emittedText || !fullText.startsWith(emittedText)) return;
	emit({
		type,
		id,
		delta: fullText.slice(emittedText.length)
	});
}
/**
* Translates an OpenCode `message.part.delta` event (a streaming text or
* reasoning delta) into legacy stream parts.
*/
function emitLegacyPartDelta({ props, state, emit }) {
	const field = String(props.field ?? "");
	const delta = String(props.delta ?? "");
	if (!delta) return;
	const messageID = stringValue$1(props.messageID);
	if (messageID && state.messageRoles.get(messageID) === "user") return;
	if (field === "text") {
		const id = legacyPartId({
			value: props,
			fallback: "legacy-text"
		});
		if (state.legacyReasoningPartIds.has(id)) {
			state.reasoningDeltas.set(id, `${state.reasoningDeltas.get(id) ?? ""}${delta}`);
			emit({
				type: "reasoning-delta",
				id,
				delta
			});
			return;
		}
		startLegacyPart({
			ids: state.legacyTextPartIds,
			id,
			emit,
			type: "text"
		});
		state.textDeltas.set(id, `${state.textDeltas.get(id) ?? ""}${delta}`);
		emit({
			type: "text-delta",
			id,
			delta
		});
		return;
	}
	if (field === "reasoning") {
		const id = legacyPartId({
			value: props,
			fallback: "legacy-reasoning"
		});
		startLegacyPart({
			ids: state.legacyReasoningPartIds,
			id,
			emit,
			type: "reasoning"
		});
		state.reasoningDeltas.set(id, `${state.reasoningDeltas.get(id) ?? ""}${delta}`);
		emit({
			type: "reasoning-delta",
			id,
			delta
		});
	}
}
/**
* Translates an OpenCode `message.part.updated` event for a text or reasoning
* part. Returns `true` when it handled the part.
*/
function emitLegacyTextPartUpdate({ part, state, emit }) {
	const textPart = legacyTextPartFromValue(part);
	if (!textPart) return false;
	const id = stringValue$1(textPart.id);
	if (!id) return true;
	const messageID = stringValue$1(textPart.messageID);
	if (messageID && state.messageRoles.get(messageID) === "user") return true;
	const isReasoning = textPart.type === "reasoning";
	const ids = isReasoning ? state.legacyReasoningPartIds : state.legacyTextPartIds;
	const deltaMap = isReasoning ? state.reasoningDeltas : state.textDeltas;
	const deltaType = isReasoning ? "reasoning-delta" : "text-delta";
	const text = typeof textPart.text === "string" ? textPart.text : void 0;
	startLegacyPart({
		ids,
		id,
		emit,
		type: isReasoning ? "reasoning" : "text"
	});
	if (text !== void 0) {
		emitMissingFinalDelta({
			id,
			fullText: text,
			emittedText: deltaMap.get(id) ?? "",
			emit,
			type: deltaType
		});
		deltaMap.set(id, text);
	}
	if (textPart.time?.end != null) {
		ids.delete(id);
		deltaMap.delete(id);
		emit({
			type: isReasoning ? "reasoning-end" : "text-end",
			id
		});
	}
	return true;
}
function stripSyncVersion(type) {
	return type.replace(/\.\d+$/, "");
}
function stringValue$1(value) {
	return typeof value === "string" && value.length > 0 ? value : void 0;
}
function legacyPartId({ value, fallback }) {
	return stringValue$1(value.partID) ?? stringValue$1(value.id) ?? fallback;
}
function startLegacyPart({ ids, id, emit, type }) {
	if (ids.has(id)) return;
	ids.add(id);
	emit({
		type: `${type}-start`,
		id
	});
}
function openCodeMessageInfoFromValue(value) {
	return asOpenCodeObject(value);
}
function openCodeEventPropertiesFromValue(value) {
	return asOpenCodeObject(value);
}
function legacyTextPartFromValue(value) {
	const part = asOpenCodeObject(value);
	if (!part || part.type !== "text" && part.type !== "reasoning") return;
	return {
		type: part.type,
		id: part.id,
		messageID: part.messageID,
		text: part.text,
		time: asOpenCodeObject(part.time)
	};
}
//#endregion
//#region src/bridge/opencode-context-fallback.ts
function createAssistantSnapshotBaseline(assistant) {
	return {
		assistantExisted: assistant != null,
		...typeof assistant?.id === "string" ? { assistantId: assistant.id } : {}
	};
}
function isAssistantSnapshotAfterBaseline({ assistant, baseline }) {
	if (typeof assistant.id !== "string") return false;
	if (!baseline.assistantExisted) return true;
	return baseline.assistantId != null && assistant.id !== baseline.assistantId;
}
//#endregion
//#region src/bridge/opencode-usage.ts
function mapUsage(tokens) {
	const value = extractOpenCodeTokens(tokens) ?? zeroOpenCodeTokens();
	const cacheRead = value.cache.read;
	return {
		inputTokens: {
			total: value.input,
			noCache: Math.max(0, value.input - cacheRead),
			cacheRead,
			cacheWrite: value.cache.write
		},
		outputTokens: {
			total: value.output + value.reasoning,
			text: value.output,
			reasoning: value.reasoning
		}
	};
}
function defaultUsage() {
	return mapUsage(zeroOpenCodeTokens());
}
function extractSessionTokens(value) {
	const record = asOpenCodeObject(value);
	if (!record) return void 0;
	return extractOpenCodeTokens(record.tokens) ?? extractOpenCodeTokens(record.info?.tokens) ?? extractOpenCodeTokens(record.data?.tokens) ?? extractOpenCodeTokens(record.data?.data?.tokens);
}
function subtractSessionTokens({ before, after }) {
	return {
		input: diff({
			before: before.input,
			after: after.input
		}),
		output: diff({
			before: before.output,
			after: after.output
		}),
		reasoning: diff({
			before: before.reasoning,
			after: after.reasoning
		}),
		cache: {
			read: diff({
				before: before.cache.read,
				after: after.cache.read
			}),
			write: diff({
				before: before.cache.write,
				after: after.cache.write
			})
		}
	};
}
function addUsage({ left, right }) {
	if (left == null) return right;
	const leftInput = asTokenGroup(left.inputTokens);
	const rightInput = asTokenGroup(right.inputTokens);
	const leftOutput = asTokenGroup(left.outputTokens);
	const rightOutput = asTokenGroup(right.outputTokens);
	return {
		inputTokens: {
			total: add({
				left: leftInput.total,
				right: rightInput.total
			}),
			noCache: add({
				left: leftInput.noCache,
				right: rightInput.noCache
			}),
			cacheRead: add({
				left: leftInput.cacheRead,
				right: rightInput.cacheRead
			}),
			cacheWrite: add({
				left: leftInput.cacheWrite,
				right: rightInput.cacheWrite
			})
		},
		outputTokens: {
			total: add({
				left: leftOutput.total,
				right: rightOutput.total
			}),
			text: add({
				left: leftOutput.text,
				right: rightOutput.text
			}),
			reasoning: add({
				left: leftOutput.reasoning,
				right: rightOutput.reasoning
			})
		}
	};
}
function extractOpenCodeTokens(value) {
	const record = asOpenCodeObject(value);
	const cache = asOpenCodeObject(record?.cache);
	if (!record || !cache) return void 0;
	return {
		input: numberValue(record.input),
		output: numberValue(record.output),
		reasoning: numberValue(record.reasoning),
		cache: {
			read: numberValue(cache.read),
			write: numberValue(cache.write)
		}
	};
}
function zeroOpenCodeTokens() {
	return {
		input: 0,
		output: 0,
		reasoning: 0,
		cache: {
			read: 0,
			write: 0
		}
	};
}
function asTokenGroup(value) {
	return asOpenCodeObject(value) ?? {};
}
function numberValue(value) {
	return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
function diff({ before, after }) {
	return Math.max(0, after - before);
}
function add({ left, right }) {
	const leftNumber = typeof left === "number" ? left : void 0;
	const rightNumber = typeof right === "number" ? right : void 0;
	return leftNumber == null && rightNumber == null ? void 0 : (leftNumber ?? 0) + (rightNumber ?? 0);
}
//#endregion
//#region src/bridge/opencode-finish-step.ts
function mapOpenCodeFinishReason(reason) {
	const normalized = reason.toLowerCase();
	if (normalized.includes("length")) return "length";
	if (normalized.includes("filter")) return "content-filter";
	if (normalized.includes("tool")) return "tool-calls";
	if (normalized.includes("error") || normalized.includes("fail")) return "error";
	if (normalized === "stop" || normalized === "end") return "stop";
	return "other";
}
function translateLegacyStepFinishPart(value) {
	const part = asOpenCodeObject(value);
	if (!part || part.type !== "step-finish") return void 0;
	const rawFinish = typeof part.reason === "string" ? part.reason : "stop";
	return {
		event: {
			type: "finish-step",
			finishReason: {
				unified: mapOpenCodeFinishReason(rawFinish),
				raw: rawFinish
			},
			usage: mapUsage(part.tokens),
			...typeof part.cost === "number" ? { harnessMetadata: { opencode: { cost: part.cost } } } : {}
		},
		...typeof part.id === "string" ? { partId: part.id } : {}
	};
}
//#endregion
//#region src/bridge/create-emit-stream-event.ts
function createEmitStreamEvent({ state, emit, emitWarning, emitError, toWireToolName, nativeNameField, getHostToolName, authorizeHostToolCall, onSubagentSession, isMcpToolName, stripWorkDir, formatError }) {
	const compactionMessages = /* @__PURE__ */ new Set();
	let compactionTrigger = "auto";
	let compaction;
	const finishCompaction = (failed = false) => {
		if (!compaction) return;
		if (failed) emit({
			type: "raw",
			rawValue: {
				type: "opencode.compaction",
				messageId: compaction.id,
				status: "failed"
			}
		});
		else emit({
			type: "compaction",
			trigger: compaction.trigger,
			summary: [...compaction.text.values()].join(""),
			harnessMetadata: { opencode: { messageId: compaction.id } }
		});
		compaction = void 0;
	};
	return (event) => {
		const type = event.type;
		const props = event.properties ?? {};
		if (type === "message.updated") {
			const info = openCodeMessageInfoFromValue(props.info);
			if (info) {
				const id = stringValue(info.id);
				const role = stringValue(info.role);
				if (id && role) state.messageRoles.set(id, role);
				if (id && info.summary === true) {
					if (!compactionMessages.has(id)) {
						compactionMessages.add(id);
						compaction = {
							id,
							trigger: compactionTrigger,
							text: /* @__PURE__ */ new Map()
						};
						emitOpenCodeStreamStart({
							info,
							state,
							emit
						});
						emit({
							type: "raw",
							rawValue: {
								type: "opencode.compaction",
								messageId: id,
								status: "started"
							}
						});
					}
					if (info.error && compaction?.id === id) finishCompaction(true);
				}
			}
			return;
		}
		if (type === "session.compacted") {
			finishCompaction();
			return;
		}
		if (type === "message.part.updated" || type === "message.part.delta") {
			const part = asOpenCodeObject(props.part);
			if (type === "message.part.updated" && part?.type === "compaction") {
				compactionTrigger = part.auto === false ? "manual" : "auto";
				return;
			}
			const messageId = stringValue(type === "message.part.updated" ? part?.messageID : props.messageID);
			if (messageId && compactionMessages.has(messageId)) {
				if (type === "message.part.updated") {
					if (messageId === compaction?.id && part?.type === "text") {
						const id = stringValue(part.id);
						if (id && typeof part.text === "string") compaction.text.set(id, part.text);
					}
					emitLegacyStepFinishPart({
						part: props.part,
						state,
						emit
					});
				} else if (messageId === compaction?.id && props.field === "text") {
					const id = stringValue(props.partID);
					if (id && compaction.text.has(id) && typeof props.delta === "string") compaction.text.set(id, compaction.text.get(id) + props.delta);
				}
				return;
			}
		}
		if (type === "message.part.delta") {
			emitLegacyPartDelta({
				props,
				state,
				emit
			});
			return;
		}
		if (type === "message.part.updated") {
			if (emitLegacyTextPartUpdate({
				part: props.part,
				state,
				emit
			})) return;
			if (emitLegacyStepFinishPart({
				part: props.part,
				state,
				emit
			})) return;
			emitLegacyToolPart({
				part: props.part,
				state,
				emit,
				toWireToolName,
				nativeNameField,
				getHostToolName,
				authorizeHostToolCall,
				onSubagentSession,
				isMcpToolName
			});
			return;
		}
		if (type === "session.next.text.started") {
			emit({
				type: "text-start",
				id: String(props.textID ?? event.id)
			});
			return;
		}
		if (type === "session.next.text.delta") {
			const id = String(props.textID ?? event.id);
			state.textDeltas.set(id, `${state.textDeltas.get(id) ?? ""}${String(props.delta ?? "")}`);
			emit({
				type: "text-delta",
				id,
				delta: String(props.delta ?? "")
			});
			return;
		}
		if (type === "session.next.text.ended") {
			const id = String(props.textID ?? event.id);
			emitMissingFinalDelta({
				id,
				fullText: typeof props.text === "string" ? props.text : void 0,
				emittedText: state.textDeltas.get(id) ?? "",
				emit,
				type: "text-delta"
			});
			emit({
				type: "text-end",
				id
			});
			return;
		}
		if (type === "session.next.reasoning.started") {
			emit({
				type: "reasoning-start",
				id: String(props.reasoningID ?? event.id)
			});
			return;
		}
		if (type === "session.next.reasoning.delta") {
			const id = String(props.reasoningID ?? event.id);
			state.reasoningDeltas.set(id, `${state.reasoningDeltas.get(id) ?? ""}${String(props.delta ?? "")}`);
			emit({
				type: "reasoning-delta",
				id,
				delta: String(props.delta ?? "")
			});
			return;
		}
		if (type === "session.next.reasoning.ended") {
			const id = String(props.reasoningID ?? event.id);
			emitMissingFinalDelta({
				id,
				fullText: typeof props.text === "string" ? props.text : void 0,
				emittedText: state.reasoningDeltas.get(id) ?? "",
				emit,
				type: "reasoning-delta"
			});
			emit({
				type: "reasoning-end",
				id
			});
			return;
		}
		if (type === "session.next.shell.started") {
			const callID = String(props.callID ?? event.id);
			const command = String(props.command ?? "");
			state.shellCommands.set(callID, command);
			emit({
				type: "tool-call",
				toolCallId: callID,
				toolName: "bash",
				nativeName: "bash",
				input: JSON.stringify({ command }),
				providerExecuted: true
			});
			return;
		}
		if (type === "session.next.shell.ended") {
			const callID = String(props.callID ?? event.id);
			emit({
				type: "tool-result",
				toolCallId: callID,
				toolName: "bash",
				result: {
					command: state.shellCommands.get(callID) ?? "",
					output: String(props.output ?? "")
				}
			});
			return;
		}
		if (type === "session.next.tool.input.delta") {
			const callID = String(props.callID ?? event.id);
			state.toolInputs.set(callID, `${state.toolInputs.get(callID) ?? ""}${String(props.delta ?? "")}`);
			return;
		}
		if (type === "session.next.tool.input.ended") {
			state.toolInputs.set(String(props.callID ?? event.id), String(props.text ?? ""));
			return;
		}
		if (type === "session.next.tool.called") {
			const callID = String(props.callID ?? event.id);
			const rawToolName = String(props.tool ?? "unknown");
			if (rawToolName === "StructuredOutput" || rawToolName === "question") return;
			const toolName = toWireToolName(rawToolName);
			state.toolNames.set(callID, {
				rawToolName,
				toolName
			});
			const hostToolName = getHostToolName(toolName, props.tool);
			if (hostToolName) {
				authorizeHostToolCall({
					callID,
					toolName: hostToolName,
					input: props.input ?? parseToolInput(state, props)
				});
				return;
			}
			emit({
				type: "tool-call",
				toolCallId: callID,
				toolName,
				...nativeNameField({
					nativeName: rawToolName,
					toolName
				}),
				input: JSON.stringify(props.input ?? parseToolInput(state, props)),
				providerExecuted: true,
				...isMcpToolName(rawToolName) ? { dynamic: true } : {},
				...props.provider?.metadata ? { providerMetadata: props.provider.metadata } : {}
			});
			if (isMcpToolName(rawToolName)) state.dynamicToolCallIds.add(callID);
			return;
		}
		if (type === "session.next.tool.success" || type === "session.next.tool.failed") {
			const callID = String(props.callID ?? event.id);
			const cachedTool = state.toolNames.get(callID);
			const rawToolName = cachedTool?.rawToolName ?? String(props.tool ?? "");
			if (rawToolName === "StructuredOutput" || rawToolName === "question") return;
			const toolName = cachedTool?.toolName ?? toWireToolName(rawToolName || "unknown");
			if (getHostToolName(toolName, rawToolName)) return;
			emit({
				type: "tool-result",
				toolCallId: callID,
				toolName,
				result: props.result ?? props.structured ?? ("content" in props ? props.content : null) ?? null,
				...type === "session.next.tool.failed" ? { isError: true } : {},
				...state.dynamicToolCallIds.delete(callID) ? { dynamic: true } : {}
			});
			return;
		}
		if (type === "session.next.retried") {
			const error = props.error ?? event;
			if (openCodeErrorFromValue(error)?.isRetryable === false) emitError({
				error,
				message: "OpenCode session retry failed"
			});
			else emitWarning({ message: nextRetryEventMessage({
				event,
				formatError
			}) });
			return;
		}
		if (type === "session.next.step.ended") {
			closeLegacyOpenParts({
				state,
				emit
			});
			state.turnUsage = mapUsage(props.tokens);
			emit({
				type: "finish-step",
				finishReason: {
					unified: mapOpenCodeFinishReason(String(props.finish ?? "stop")),
					raw: String(props.finish ?? "stop")
				},
				usage: state.turnUsage,
				...typeof props.cost === "number" ? { harnessMetadata: { opencode: { cost: props.cost } } } : {}
			});
			return;
		}
		if (type === "session.next.compaction.ended") {
			emit({
				type: "compaction",
				trigger: props.reason === "auto" ? "auto" : "manual",
				summary: String(props.text ?? ""),
				harnessMetadata: { opencode: { recent: String(props.recent ?? "") } }
			});
			return;
		}
		if (type === "file.edited") {
			emit({
				type: "file-change",
				event: "modify",
				path: stripWorkDir(String(props.file ?? ""))
			});
			return;
		}
		if (type === "session.error" || type === "session.next.step.failed") {
			finishCompaction(true);
			emitError({
				error: props.error ?? event,
				message: type === "session.error" ? "OpenCode session error" : "OpenCode step failed"
			});
		}
	};
}
function closeLegacyOpenParts({ state, emit }) {
	for (const id of state.legacyReasoningPartIds) {
		emit({
			type: "reasoning-end",
			id
		});
		state.reasoningDeltas.delete(id);
	}
	state.legacyReasoningPartIds.clear();
	for (const id of state.legacyTextPartIds) {
		emit({
			type: "text-end",
			id
		});
		state.textDeltas.delete(id);
	}
	state.legacyTextPartIds.clear();
}
function emitLegacyStepFinishPart({ part, state, emit }) {
	const translated = translateLegacyStepFinishPart(part);
	if (!translated) return false;
	const { event, partId: id } = translated;
	if (id) {
		if (state.legacyStepFinishPartIds.has(id)) return true;
		state.legacyStepFinishPartIds.add(id);
	}
	closeLegacyOpenParts({
		state,
		emit
	});
	state.turnUsage = event.usage;
	emit(event);
	return true;
}
function emitLegacyToolPart({ part, state, emit, toWireToolName, nativeNameField, getHostToolName, authorizeHostToolCall, onSubagentSession, isMcpToolName }) {
	const toolPart = legacyToolPartFromValue(part);
	if (!toolPart) return;
	const status = toolPart.status;
	if (status !== "running" && status !== "completed" && status !== "error") return;
	const callID = toolPart.callID;
	const rawToolName = toolPart.tool;
	if (rawToolName === "StructuredOutput" || rawToolName === "question") return;
	const toolName = toWireToolName(rawToolName);
	if (toolName === "agent") {
		const metadata = {
			...toolPart.metadata,
			...toolPart.state?.metadata
		};
		const parentSessionId = stringValue(metadata.parentSessionId);
		const sessionId = stringValue(metadata.sessionId);
		if (parentSessionId && sessionId) onSubagentSession?.({
			parentSessionId,
			sessionId
		});
	}
	state.toolNames.set(callID, {
		rawToolName,
		toolName
	});
	const hostToolName = getHostToolName(toolName, rawToolName);
	if (hostToolName) {
		if (status === "running") authorizeHostToolCall({
			callID,
			toolName: hostToolName,
			input: toolPart.state?.input ?? {}
		});
		return;
	}
	if (!state.toolCallsEmitted.has(callID)) {
		state.toolCallsEmitted.add(callID);
		emit({
			type: "tool-call",
			toolCallId: callID,
			toolName,
			...nativeNameField({
				nativeName: rawToolName,
				toolName
			}),
			input: JSON.stringify(legacyToolPartInput(toolPart)),
			providerExecuted: true,
			...isMcpToolName(rawToolName) ? { dynamic: true } : {},
			...toolPart.providerMetadata ? { providerMetadata: toolPart.providerMetadata } : {}
		});
		if (isMcpToolName(rawToolName)) state.dynamicToolCallIds.add(callID);
	}
	if ((status === "completed" || status === "error") && !state.toolResultsEmitted.has(callID)) {
		state.toolResultsEmitted.add(callID);
		emit({
			type: "tool-result",
			toolCallId: callID,
			toolName,
			result: legacyToolPartOutput(toolPart),
			...status === "error" ? { isError: true } : {},
			...state.dynamicToolCallIds.delete(callID) ? { dynamic: true } : {}
		});
	}
}
function legacyToolPartInput(part) {
	return {
		...part.metadata,
		...part.state?.metadata,
		...part.state?.input
	};
}
function legacyToolPartOutput(part) {
	const state = part.state;
	if (state?.status === "error") return state.error ?? part.error ?? state.result ?? "tool failed";
	return state?.output ?? state?.result ?? state?.structured ?? state?.content ?? null;
}
function parseToolInput(state, props) {
	const text = state.toolInputs.get(String(props.callID ?? ""));
	if (!text) return {};
	try {
		return JSON.parse(text);
	} catch {
		return { input: text };
	}
}
function nextRetryEventMessage({ event, formatError }) {
	const props = event.properties ?? {};
	const details = [];
	if (typeof props.attempt === "number") details.push(`attempt ${props.attempt}`);
	const error = props.error;
	const errorDetails = openCodeErrorFromValue(error);
	if (errorDetails) {
		const message = stringValue(errorDetails.message) ?? stringValue(errorDetails.data?.message);
		const statusCode = errorDetails.statusCode;
		if (typeof statusCode === "number") details.push(`HTTP ${statusCode}`);
		if (message) details.push(message);
	} else if (error != null) details.push(formatError(error));
	return details.length > 0 ? `OpenCode session retry: ${details.join("; ")}` : "OpenCode session retry";
}
function stringValue(value) {
	return typeof value === "string" && value.length > 0 ? value : void 0;
}
function legacyToolPartFromValue(value) {
	const part = asOpenCodeObject(value);
	if (!part || part.type !== "tool" || typeof part.tool !== "string" || typeof part.callID !== "string") return;
	const stateObject = asOpenCodeObject(part.state);
	const provider = asOpenCodeObject(part.provider);
	const state = stateObject ? {
		status: stringValue(stateObject.status),
		input: asOpenCodeObject(stateObject.input),
		metadata: asOpenCodeObject(stateObject.metadata),
		output: stateObject.output,
		result: stateObject.result,
		structured: stateObject.structured,
		content: stateObject.content,
		error: stateObject.error
	} : void 0;
	return {
		type: "tool",
		callID: part.callID,
		tool: part.tool,
		status: typeof part.state === "string" ? part.state : state?.status ?? void 0,
		state,
		metadata: asOpenCodeObject(part.metadata),
		providerMetadata: provider?.metadata,
		error: part.error
	};
}
function openCodeErrorFromValue(value) {
	const error = asOpenCodeObject(value);
	if (!error) return void 0;
	const data = asOpenCodeObject(error.data);
	return {
		isRetryable: error.isRetryable,
		message: error.message,
		statusCode: error.statusCode,
		data: data ? { message: data.message } : void 0
	};
}
//#endregion
//#region src/bridge/opencode-path.ts
const fallbackPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin";
function prependOpenCodeBinToPath({ bootstrapDir, env }) {
	env.PATH = [path.join(bootstrapDir, "node_modules", ".bin"), env.PATH || fallbackPath].join(path.delimiter);
}
//#endregion
//#region src/bridge/opencode-server-auth.ts
function configureOpenCodeServerAuth({ env }) {
	const username = env.OPENCODE_SERVER_USERNAME ?? "opencode";
	const password = randomBytes(32).toString("hex");
	env.OPENCODE_SERVER_PASSWORD = password;
	return { Authorization: `Basic ${Buffer.from(`${username}:${password}`).toString("base64")}` };
}
//#endregion
//#region src/bridge/tool-relay-auth.ts
var ToolRelayAuthorizer = class {
	constructor({ ttlMs = 1e4, now = Date.now } = {}) {
		this.authorizations = [];
		this.pendingRequests = [];
		this.ttlMs = ttlMs;
		this.now = now;
	}
	authorizeToolCall(call) {
		this.pruneExpired();
		const key = toolRelayCallKey(call);
		const pendingRequestIndex = this.pendingRequests.findIndex((request) => request.key === key);
		if (pendingRequestIndex !== -1) {
			const [pendingRequest] = this.pendingRequests.splice(pendingRequestIndex, 1);
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
		const authorizationIndex = this.authorizations.findIndex((authorization) => authorization.key === key);
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
		for (let i = this.authorizations.length - 1; i >= 0; i--) if (this.authorizations[i].expiresAt <= now) this.authorizations.splice(i, 1);
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
function toolRelayCallKey({ toolName, input }) {
	return `${toolName}\0${canonicalJson(input ?? {})}`;
}
function canonicalJson(value) {
	return JSON.stringify(normalizeJsonValue(value));
}
function normalizeJsonValue(value) {
	if (Array.isArray(value)) return value.map(normalizeJsonValue);
	if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).filter(([, entryValue]) => entryValue !== void 0).sort(([left], [right]) => left.localeCompare(right)).map(([key, entryValue]) => [key, normalizeJsonValue(entryValue)]));
	return value;
}
//#endregion
//#region src/bridge/tool-relay.ts
async function startAuthorizedToolRelay({ tools, emit, requestToolResult, authorizer = new ToolRelayAuthorizer() }) {
	const toolNames = new Set(tools.map((tool) => tool.name));
	const server = createServer(async (req, res) => {
		try {
			if (req.method !== "POST" || req.url !== "/") {
				res.writeHead(401, { "Content-Type": "application/json" });
				res.end(JSON.stringify({ error: "unauthorized tool relay request" }));
				return;
			}
			const chunks = [];
			for await (const chunk of req) chunks.push(chunk);
			const body = Buffer.concat(chunks).toString("utf8");
			const { requestId, toolName, input } = JSON.parse(body);
			if (!toolNames.has(toolName)) {
				res.writeHead(403, { "Content-Type": "application/json" });
				res.end(JSON.stringify({ error: `Tool "${toolName}" is not available` }));
				return;
			}
			if (!await authorizer.waitForToolCallAuthorization({
				toolName,
				input
			})) {
				res.writeHead(401, { "Content-Type": "application/json" });
				res.end(JSON.stringify({ error: "unauthorized tool relay request" }));
				return;
			}
			emit({
				type: "tool-call",
				toolCallId: requestId,
				toolName,
				input: JSON.stringify(input ?? {}),
				providerExecuted: false
			});
			const { output, isError } = await requestToolResult(requestId);
			emit({
				type: "tool-result",
				toolCallId: requestId,
				toolName,
				result: output ?? null,
				isError: !!isError
			});
			res.writeHead(200, { "Content-Type": "application/json" });
			res.end(JSON.stringify({ result: output }));
		} catch (error) {
			res.writeHead(500, { "Content-Type": "application/json" });
			res.end(JSON.stringify({ error: error instanceof Error ? error.message : String(error) }));
		}
	});
	await new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
	const address = server.address();
	if (!address || typeof address === "string") throw new Error("tool relay did not expose a numeric port");
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
	} catch {}
}
//#endregion
//#region src/bridge/question-tool.ts
function openCodeQuestionKey(nativeRequest) {
	return JSON.stringify(nativeRequest.questions);
}
function toHarnessQuestionsInput(nativeRequest) {
	return {
		allowPartialAnswers: true,
		questions: nativeRequest.questions.map((question, questionIndex) => ({
			id: `question-${questionIndex + 1}`,
			question: question.question,
			header: question.header,
			options: question.options.map((option, optionIndex) => ({
				id: `option-${optionIndex + 1}`,
				label: option.label,
				description: option.description
			})),
			...question.multiple !== void 0 ? { allowMultiple: question.multiple } : {},
			...question.custom !== void 0 ? { allowFreeForm: question.custom } : {}
		}))
	};
}
function toOpenCodeQuestionResponse(input) {
	if (input.output.action === "declined" || input.output.action === "cancelled") return { action: "reject" };
	return {
		action: "reply",
		answers: input.nativeRequest.questions.map((nativeQuestion, questionIndex) => {
			const answer = input.output.action === "answered" || input.output.action === "partially-answered" ? input.output.answers[`question-${questionIndex + 1}`] : void 0;
			if (answer == null) return [];
			const selectedLabels = answer.optionIds.flatMap((optionId) => {
				const index = positionalIdIndex({
					id: optionId,
					prefix: "option-"
				});
				const option = index == null ? void 0 : nativeQuestion.options[index];
				return option == null ? [] : [option.label];
			});
			return answer.freeform === void 0 ? selectedLabels : [...selectedLabels, answer.freeform];
		})
	};
}
function positionalIdIndex(input) {
	if (!input.id.startsWith(input.prefix)) return void 0;
	const oneBasedIndex = Number(input.id.slice(input.prefix.length));
	return Number.isInteger(oneBasedIndex) && oneBasedIndex > 0 ? oneBasedIndex - 1 : void 0;
}
//#endregion
//#region src/bridge/index.ts
const NATIVE_TO_COMMON = {
	view: "read",
	read: "read",
	write: "write",
	edit: "edit",
	bash: "bash",
	glob: "glob",
	grep: "grep",
	question: "askUserQuestions"
};
const OPENCODE_TO_WIRE = {
	list: "ls",
	ls: "ls",
	webfetch: "webfetch",
	task: "agent",
	agent: "agent",
	askUserQuestions: "question",
	subtask: "agent"
};
const PUBLIC_TO_NATIVE = {
	read: "view",
	write: "write",
	edit: "edit",
	bash: "bash",
	glob: "glob",
	grep: "grep",
	ls: "list",
	webfetch: "webfetch",
	skill: "skill",
	todowrite: "todowrite",
	agent: "agent"
};
const TOOL_KIND = {
	read: "readonly",
	glob: "readonly",
	grep: "readonly",
	ls: "readonly",
	webfetch: "readonly",
	write: "edit",
	edit: "edit",
	bash: "bash",
	agent: "bash",
	skill: "edit",
	todowrite: "edit"
};
const HARNESS_CLIENT_APP = env.AI_SDK_HARNESS_CLIENT_APP;
const args = parseArgs(argv.slice(2));
const workdir = args.workdir ?? emitFatal("Missing --workdir argument.");
const bridgeStateDir = args.bridgeStateDir ?? emitFatal("Missing --bridge-state-dir argument.");
const bootstrapDir = args.bootstrapDir ?? workdir;
const skillsDir = args.skillsDir;
const runtime = {
	toolNames: /* @__PURE__ */ new Set(),
	mcpToolPrefixes: /* @__PURE__ */ new Set()
};
prependOpenCodeBinToPath({
	bootstrapDir,
	env
});
await runBridge({
	bridgeType: "opencode",
	bridgeStateDir,
	onStart: runTurn,
	onStop: () => runtime.sessionId ? { openCodeSessionId: runtime.sessionId } : {}
});
async function runTurn(start, turn) {
	const emit = (msg) => turn.emit(msg);
	let totalUsage;
	try {
		await ensureRuntime({
			start,
			turn,
			emit
		});
		const client = runtime.client;
		if (start.skillsChanged) await client.instance.dispose({ directory: workdir });
		const sessionId = await ensureSession({
			client,
			start,
			emit
		});
		await switchSessionModel({
			client,
			sessionId,
			start
		});
		if (start.operation === "compact") await runCompaction({
			client,
			sessionId,
			start,
			turn,
			emit
		});
		else totalUsage = await runPrompt({
			client,
			sessionId,
			start,
			turn,
			emit
		});
	} catch (err) {
		turn.emitError({
			error: err,
			message: "OpenCode turn failed"
		});
	} finally {
		turn.experimental_userMessages.close();
		emit({
			type: "finish",
			finishReason: {
				unified: "stop",
				raw: "stop"
			},
			totalUsage: totalUsage ?? defaultUsage()
		});
	}
}
async function switchSessionModel({ client, sessionId, start }) {
	const model = modelRefFromStart(start);
	if (model == null) return;
	const response = await client.v2.session.switchModel({
		sessionID: sessionId,
		model: {
			id: model.modelID,
			providerID: model.providerID
		}
	});
	if (response.error != null) throw response.error;
}
async function ensureRuntime({ start, turn, emit }) {
	if (runtime.client && isDeepStrictEqual(runtime.openCodeConfig, start.openCodeConfig)) return;
	closeRuntime();
	try {
		if (start.tools && start.tools.length > 0) {
			runtime.toolNames = new Set(start.tools.map((tool) => tool.name));
			runtime.relay = await startToolRelay({
				tools: start.tools,
				emit,
				requestToolResult: turn.requestToolResult
			});
		}
		const serverAuthHeaders = configureOpenCodeServerAuth({ env });
		const server = await createOpencodeServer({
			hostname: "127.0.0.1",
			port: 0,
			timeout: 3e4,
			config: buildOpenCodeConfig({
				start,
				relayPort: runtime.relay?.port
			})
		});
		runtime.server = server;
		runtime.client = createOpencodeClient({
			baseUrl: server.url,
			directory: workdir,
			headers: serverAuthHeaders
		});
		const mcpServers = asOpenCodeObject((await runtime.client.mcp.status()).data) ?? {};
		runtime.mcpToolPrefixes = new Set(Object.entries(mcpServers).filter(([serverName, status]) => serverName !== "harness-tools" && asOpenCodeObject(status)?.status === "connected").map(([serverName]) => `${sanitizeMcpToolName(serverName)}_`));
		runtime.openCodeConfig = structuredClone(start.openCodeConfig);
	} catch (error) {
		closeRuntime();
		throw error;
	}
}
function closeRuntime() {
	runtime.relay?.close();
	runtime.server?.close();
	runtime.server = void 0;
	runtime.client = void 0;
	runtime.relay = void 0;
	runtime.openCodeConfig = void 0;
	runtime.toolNames = /* @__PURE__ */ new Set();
	runtime.mcpToolPrefixes = /* @__PURE__ */ new Set();
}
function buildOpenCodeConfig({ start, relayPort }) {
	const config = {
		...withoutAgentPolicyOverrides(start.openCodeConfig),
		share: "disabled",
		autoupdate: false,
		permission: {
			read: "allow",
			glob: "allow",
			grep: "allow",
			list: "allow",
			edit: "ask",
			bash: "ask",
			external_directory: "ask",
			webfetch: "ask",
			doom_loop: "ask",
			task: "ask",
			question: "allow"
		}
	};
	if (start.model) config.model = start.model;
	if (skillsDir) config.skills = { paths: [skillsDir] };
	const inactiveToolNames = resolveInactiveBuiltinToolNames(start);
	const permission = config.permission;
	for (const toolName of inactiveToolNames) {
		const permissionName = toPermissionToolName(PUBLIC_TO_NATIVE[toolName] ?? toolName);
		if (permissionName === "ls") permission.list = "ask";
		else permission[permissionName] = "ask";
	}
	const provider = buildProviderConfig(start);
	if (provider) config.provider = provider;
	const mcp = { ...start.mcpServers };
	if (relayPort && start.tools && start.tools.length > 0) mcp["harness-tools"] = {
		type: "local",
		enabled: true,
		command: ["node", `${bootstrapDir}/host-tool-mcp.mjs`],
		environment: {
			TOOL_SCHEMAS: JSON.stringify(start.tools.map((t) => ({
				name: t.name,
				description: t.description,
				inputSchema: t.inputSchema
			}))),
			TOOL_RELAY_URL: `http://127.0.0.1:${relayPort}`
		}
	};
	if (Object.keys(mcp).length > 0) config.mcp = mcp;
	return config;
}
function withoutAgentPolicyOverrides(input) {
	const config = { ...input };
	for (const key of ["agent", "mode"]) {
		const agents = asOpenCodeObject(config[key]);
		if (!agents) continue;
		config[key] = Object.fromEntries(Object.entries(agents).map(([name, value]) => {
			const agent = asOpenCodeObject(value);
			if (!agent) return [name, value];
			const safeAgent = { ...agent };
			delete safeAgent.permission;
			delete safeAgent.tools;
			return [name, safeAgent];
		}));
	}
	return config;
}
function buildProviderConfig(start) {
	const model = splitModel(start.model, start.provider);
	const providerID = model.providerID ?? start.provider ?? env.OPENAI_NAME ?? "anthropic";
	const modelID = model.modelID;
	if (env.AI_GATEWAY_API_KEY && env.AI_GATEWAY_BASE_URL) return { [providerID]: {
		options: {
			apiKey: env.AI_GATEWAY_API_KEY,
			baseURL: toOpenCodeGatewayBaseUrl(env.AI_GATEWAY_BASE_URL),
			...HARNESS_CLIENT_APP ? { headers: {
				...start.headers,
				"x-client-app": HARNESS_CLIENT_APP
			} } : start.headers ? { headers: start.headers } : {}
		},
		...modelID ? { models: { [modelID]: {
			id: modelID,
			name: modelID
		} } } : {}
	} };
	if ((env.OPENAI_NAME || providerID !== "anthropic" && providerID !== "openai") && (env.OPENAI_API_KEY || env.OPENAI_BASE_URL)) return { [env.OPENAI_NAME ?? providerID]: {
		options: {
			...env.OPENAI_API_KEY ? { apiKey: env.OPENAI_API_KEY } : {},
			...env.OPENAI_BASE_URL ? { baseURL: env.OPENAI_BASE_URL } : {},
			...start.headers ? { headers: start.headers } : {},
			...parseOpenAIQueryParams()
		},
		...modelID ? { models: { [modelID]: {
			id: modelID,
			name: modelID
		} } } : {}
	} };
	if (providerID === "anthropic" && (env.ANTHROPIC_API_KEY || env.ANTHROPIC_AUTH_TOKEN || env.ANTHROPIC_BASE_URL)) return { anthropic: { options: {
		...env.ANTHROPIC_API_KEY ? { apiKey: env.ANTHROPIC_API_KEY } : {},
		...env.ANTHROPIC_AUTH_TOKEN ? { authToken: env.ANTHROPIC_AUTH_TOKEN } : {},
		...env.ANTHROPIC_BASE_URL ? { baseURL: env.ANTHROPIC_BASE_URL } : {},
		...start.headers ? { headers: start.headers } : {}
	} } };
	if (providerID === "openai" && (env.OPENAI_API_KEY || env.OPENAI_BASE_URL)) return { openai: { options: {
		...env.OPENAI_API_KEY ? { apiKey: env.OPENAI_API_KEY } : {},
		...env.OPENAI_BASE_URL ? { baseURL: env.OPENAI_BASE_URL } : {},
		...env.OPENAI_ORGANIZATION ? { organization: env.OPENAI_ORGANIZATION } : {},
		...env.OPENAI_PROJECT ? { project: env.OPENAI_PROJECT } : {},
		...start.headers ? { headers: start.headers } : {},
		...parseOpenAIQueryParams()
	} } };
}
function parseOpenAIQueryParams() {
	if (!env.OPENAI_QUERY_PARAMS_JSON) return {};
	try {
		const parsed = JSON.parse(env.OPENAI_QUERY_PARAMS_JSON);
		if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return { queryParams: parsed };
	} catch {}
	return {};
}
function toOpenCodeGatewayBaseUrl(baseUrl) {
	const trimmed = baseUrl.replace(/\/+$/, "");
	return trimmed.endsWith("/v1") ? trimmed : `${trimmed}/v1`;
}
async function legacySessionGet({ client, sessionId }) {
	const session = client.session;
	if (!session?.get) return client.v2.session.get({ sessionID: sessionId });
	return session.get({ sessionID: sessionId });
}
async function legacySessionCreate({ client }) {
	return client.session.create({});
}
async function legacySessionPrompt({ client, sessionId, start, prompt: promptText }) {
	const session = client.session;
	return (session.promptAsync ?? session.prompt).call(session, {
		sessionID: sessionId,
		...start.instructions ? { system: start.instructions } : {},
		...start.variant ? { variant: start.variant } : {},
		...start.responseFormat?.type === "json" && start.responseFormat.schema != null ? { format: {
			type: "json_schema",
			schema: start.responseFormat.schema
		} } : {},
		parts: [{
			type: "text",
			text: promptText ?? start.prompt
		}]
	});
}
async function legacySessionAbort({ client, sessionId }) {
	return client.session.abort({ sessionID: sessionId });
}
async function legacySessionSummarize({ client, sessionId, model }) {
	return client.session.summarize({
		sessionID: sessionId,
		auto: false,
		providerID: model.providerID,
		modelID: model.modelID
	});
}
async function subscribeLegacyEvents({ client, signal }) {
	return getEventStream(await client.event.subscribe(void 0, {
		signal,
		sseMaxRetryAttempts: 0
	}));
}
function readSessionId(data) {
	if (!data || typeof data !== "object") return void 0;
	const record = data;
	if (typeof record.id === "string") return record.id;
	if (typeof record.data?.id === "string") return record.data.id;
}
function isAsyncIterable(value) {
	return typeof value === "object" && value !== null && Symbol.asyncIterator in value;
}
function getEventStream(source) {
	if (!source || typeof source !== "object") return null;
	const candidate = source;
	if (isAsyncIterable(candidate.stream)) return candidate.stream;
	if (isAsyncIterable(candidate.data)) return candidate.data;
	return null;
}
function legacyStatusType(event) {
	const status = event.properties?.status;
	return status && typeof status === "object" ? String(status.type ?? "") : void 0;
}
function legacyRetryStatusMessage(event) {
	const status = event.properties?.status;
	const details = [];
	if (status && typeof status === "object") {
		const retryStatus = status;
		if (typeof retryStatus.attempt === "number") details.push(`attempt ${retryStatus.attempt}`);
		if (typeof retryStatus.message === "string" && retryStatus.message.trim()) details.push(retryStatus.message.trim());
	}
	return details.length > 0 ? `OpenCode session retry: ${details.join("; ")}` : "OpenCode session retry";
}
async function ensureSession({ client, start, emit }) {
	if (runtime.sessionId) return runtime.sessionId;
	if (start.resumeSessionId) {
		const existing = await legacySessionGet({
			client,
			sessionId: start.resumeSessionId
		}).catch(() => void 0);
		if (existing && !existing.error) {
			runtime.sessionId = start.resumeSessionId;
			emit({
				type: "bridge-thread",
				threadId: runtime.sessionId
			});
			return runtime.sessionId;
		}
	}
	const created = await legacySessionCreate({ client });
	if (created.error) throw new Error(`OpenCode session create failed: ${formatError(created.error)}`);
	const id = readSessionId(created.data);
	if (!id) throw new Error("OpenCode session create returned no id.");
	runtime.sessionId = id;
	emit({
		type: "bridge-thread",
		threadId: id
	});
	return id;
}
async function runPrompt({ client, sessionId, start, turn, emit }) {
	const eventsAbort = new AbortController();
	const turnSettled = createDeferred();
	const promptRequestSettled = createDeferred();
	let abortSessionPromise;
	const abortSession = () => {
		eventsAbort.abort();
		turn.experimental_userMessages.close();
		turnSettled.resolve("aborted");
		abortSessionPromise ??= (async () => {
			await promptRequestSettled.promise;
			const aborted = await legacySessionAbort({
				client,
				sessionId
			});
			if (aborted.error) throw new Error(`OpenCode session abort failed: ${formatError(aborted.error)}`);
		})();
		abortSessionPromise.catch(() => {});
	};
	turn.abortSignal.addEventListener("abort", abortSession, { once: true });
	if (turn.abortSignal.aborted) abortSession();
	let sawContent = false;
	let sawFinishStep = false;
	let sawBusy = false;
	let sawStructuredOutput = false;
	let terminalError;
	let submittingUserMessage = false;
	const state = createTranslationState();
	const initialSessionTokens = await readSessionTokens({
		client,
		sessionId
	}).catch(() => void 0);
	const assistantBaseline = createAssistantSnapshotBaseline(await latestAssistantSnapshot({
		client,
		sessionId
	}));
	const eventsReady = createDeferred();
	let stepUsage;
	let latestSessionTokens;
	const eventLoop = consumeEvents({
		client,
		sessionId,
		permissionMode: start.permissionMode,
		builtinToolFiltering: start.builtinToolFiltering,
		turn,
		state,
		emit: (msg) => {
			if (msg.type === "text-delta" || msg.type === "reasoning-delta") sawContent = true;
			if (msg.type === "finish-step") {
				sawFinishStep = true;
				stepUsage = addUsage({
					left: stepUsage,
					right: msg.usage
				});
			}
			emit(msg);
		},
		signal: eventsAbort.signal,
		onSubscribed: () => eventsReady.resolve(void 0),
		onEvent: (event) => {
			if (event.type === "message.updated") {
				emitOpenCodeStreamStart({
					info: event.properties?.info,
					state,
					emit
				});
				const info = asOpenCodeObject(event.properties?.info);
				if (start.responseFormat?.type === "json" && info?.structured !== void 0) {
					const id = String(info.id ?? randomUUID());
					emit({
						type: "text-start",
						id
					});
					emit({
						type: "text-delta",
						id,
						delta: JSON.stringify(info.structured)
					});
					emit({
						type: "text-end",
						id
					});
					emit({
						type: "finish-step",
						finishReason: {
							unified: "stop",
							raw: "stop"
						},
						usage: defaultUsage()
					});
					sawFinishStep = true;
					sawStructuredOutput = true;
					if (!submittingUserMessage && turn.experimental_userMessages.pendingCount === 0) {
						turn.experimental_userMessages.close();
						turnSettled.resolve("event");
						return true;
					}
				}
			}
			if (event.type === "session.updated") latestSessionTokens = extractSessionTokens(event.properties) ?? latestSessionTokens;
			if (event.type === "session.next.step.failed" || event.type === "session.error") {
				const error = formatError(event.properties?.error ?? event);
				if (event.type === "session.error") terminalError = error;
				turn.experimental_userMessages.close(new Error(error));
				turnSettled.resolve("event");
				return true;
			}
			const status = legacyStatusType(event);
			if (status === "busy") sawBusy = true;
			else if (status === "retry") {
				sawBusy = true;
				turn.emitWarning({ message: legacyRetryStatusMessage(event) });
			} else if (sawBusy && status === "idle") {
				sawBusy = false;
				if (!submittingUserMessage && turn.experimental_userMessages.pendingCount === 0 && (start.responseFormat?.type !== "json" || sawStructuredOutput)) {
					turn.experimental_userMessages.close();
					turnSettled.resolve("event");
					return true;
				}
			}
		}
	}).finally(() => {
		eventsReady.resolve(void 0);
		turn.experimental_userMessages.close(/* @__PURE__ */ new Error("OpenCode event stream ended before the turn settled."));
		turnSettled.resolve("stream-ended");
	});
	await eventsReady.promise;
	const userMessageLoop = (async () => {
		for await (const message of turn.experimental_userMessages) {
			submittingUserMessage = true;
			try {
				const prompted = await legacySessionPrompt({
					client,
					sessionId,
					start,
					prompt: message.text
				});
				if (prompted.error) {
					message.reject(/* @__PURE__ */ new Error(`OpenCode prompt failed: ${formatError(prompted.error)}`));
					continue;
				}
				message.accept();
			} catch (error) {
				message.reject(error);
			} finally {
				submittingUserMessage = false;
			}
		}
	})();
	if (!turn.abortSignal.aborted) {
		let prompted;
		try {
			prompted = await legacySessionPrompt({
				client,
				sessionId,
				start
			});
		} catch (error) {
			promptRequestSettled.resolve(void 0);
			turn.abortSignal.removeEventListener("abort", abortSession);
			eventsAbort.abort();
			turn.experimental_userMessages.close(error);
			throw error;
		}
		promptRequestSettled.resolve(void 0);
		if (prompted.error) {
			turn.abortSignal.removeEventListener("abort", abortSession);
			eventsAbort.abort();
			turn.experimental_userMessages.close(/* @__PURE__ */ new Error(`OpenCode prompt failed: ${formatError(prompted.error)}`));
			throw new Error(`OpenCode prompt failed: ${formatError(prompted.error)}`);
		}
	} else promptRequestSettled.resolve(void 0);
	const settlement = await turnSettled.promise;
	turn.abortSignal.removeEventListener("abort", abortSession);
	eventsAbort.abort();
	await eventLoop.catch(() => {});
	await userMessageLoop.catch(() => {});
	if (settlement === "aborted") {
		try {
			await abortSessionPromise;
		} catch (error) {
			closeRuntime();
			throw error;
		}
		return;
	}
	if (settlement === "stream-ended") throw new Error("OpenCode event stream ended before the turn settled.");
	if (terminalError) throw new Error(terminalError);
	if (!sawFinishStep) {
		if (!await emitContextFallback({
			client,
			sessionId,
			assistantBaseline,
			state,
			emit,
			emitContent: !sawContent
		}).catch(() => false)) throw new Error("OpenCode turn settled without a correlated assistant response.");
	}
	const finalSessionTokens = await readSessionTokens({
		client,
		sessionId
	}).catch(() => void 0) ?? latestSessionTokens;
	if (initialSessionTokens && finalSessionTokens) return mapUsage(subtractSessionTokens({
		before: initialSessionTokens,
		after: finalSessionTokens
	}));
	return stepUsage;
}
async function runCompaction({ client, sessionId, start, turn, emit }) {
	const eventsAbort = new AbortController();
	const compactionSettled = createDeferred();
	let sawCompaction = false;
	let sawBusy = false;
	let terminalError;
	const model = await resolveCompactionModel({
		client,
		sessionId,
		start
	});
	if (!model) throw new Error("OpenCode compaction requires a previous turn or an explicit model.");
	const eventLoop = consumeEvents({
		client,
		sessionId,
		permissionMode: start.permissionMode,
		builtinToolFiltering: start.builtinToolFiltering,
		turn,
		state: createTranslationState(),
		emit: (msg) => {
			if (msg.type === "compaction") sawCompaction = true;
			emit(msg);
		},
		signal: eventsAbort.signal,
		onEvent: (event) => {
			if (event.type === "session.next.compaction.ended" || event.type === "session.compacted") {
				compactionSettled.resolve();
				return true;
			}
			const status = legacyStatusType(event);
			if (status === "busy") sawBusy = true;
			else if (status === "retry") {
				sawBusy = true;
				turn.emitWarning({ message: legacyRetryStatusMessage(event) });
			} else if (sawBusy && status === "idle") {
				compactionSettled.resolve();
				return true;
			}
			if (event.type === "session.error") {
				terminalError = formatError(event.properties?.error ?? event);
				compactionSettled.resolve();
				return true;
			}
		}
	});
	const compacted = await legacySessionSummarize({
		client,
		sessionId,
		model
	});
	if (compacted.error) {
		eventsAbort.abort();
		throw new Error(`OpenCode compaction failed: ${formatError(compacted.error)}`);
	}
	await Promise.race([compactionSettled.promise, sleep(250)]);
	eventsAbort.abort();
	await eventLoop.catch(() => {});
	if (terminalError) throw new Error(terminalError);
	if (!sawCompaction) emit({
		type: "compaction",
		trigger: "manual",
		summary: "",
		harnessMetadata: { opencode: { missingSummary: true } }
	});
}
async function consumeEvents({ client, sessionId, permissionMode, builtinToolFiltering, turn, state, emit, signal, onSubscribed, onEvent }) {
	const stream = await subscribeLegacyEvents({
		client,
		signal
	});
	onSubscribed?.();
	if (!stream) return;
	const taskSessionIds = /* @__PURE__ */ new Set([sessionId]);
	const registerSubagentSession = (sourceSessionId) => function register({ parentSessionId, sessionId: subagentSessionId }) {
		if (parentSessionId === sourceSessionId && taskSessionIds.has(sourceSessionId)) taskSessionIds.add(subagentSessionId);
	};
	const emitStreamEvent = createEmitStreamEvent({
		state,
		emit,
		emitWarning: turn.emitWarning,
		emitError: turn.emitError,
		toWireToolName,
		nativeNameField,
		getHostToolName,
		authorizeHostToolCall: (input) => authorizeHostToolCall({
			...input,
			state
		}),
		onSubagentSession: registerSubagentSession(sessionId),
		isMcpToolName: (toolName) => [...runtime.mcpToolPrefixes].some((prefix) => toolName.startsWith(prefix)),
		stripWorkDir,
		formatError
	});
	const descendantEventProcessors = /* @__PURE__ */ new Map();
	const processDescendantEvent = (descendantSessionId, event) => {
		let processEvent = descendantEventProcessors.get(descendantSessionId);
		if (!processEvent) {
			const descendantState = createTranslationState();
			let currentEvent;
			let modelId;
			const emittedUsageStepIds = /* @__PURE__ */ new Set();
			processEvent = createEmitStreamEvent({
				state: descendantState,
				emit: (message) => {
					if (message.type !== "finish-step") return;
					const stepId = getSubagentStepId(currentEvent);
					if (!stepId || emittedUsageStepIds.has(stepId)) return;
					emittedUsageStepIds.add(stepId);
					const opencodeMetadata = asOpenCodeObject(message.harnessMetadata)?.opencode;
					const cost = asOpenCodeObject(opencodeMetadata)?.cost;
					emit({
						type: "raw",
						rawValue: {
							type: "opencode.subagent-usage",
							version: 1,
							sessionId: descendantSessionId,
							stepId,
							...modelId ? { modelId } : {},
							usage: message.usage,
							...typeof cost === "number" ? { cost } : {}
						}
					});
				},
				emitWarning: () => void 0,
				emitError: () => void 0,
				toWireToolName,
				nativeNameField,
				getHostToolName,
				authorizeHostToolCall: (input) => authorizeHostToolCall({
					...input,
					state: descendantState
				}),
				onSubagentSession: registerSubagentSession(descendantSessionId),
				isMcpToolName: () => false,
				stripWorkDir,
				formatError
			});
			const emitDescendantEvent = processEvent;
			processEvent = (descendantEvent) => {
				currentEvent = descendantEvent;
				if (descendantEvent.type === "message.updated") {
					const info = openCodeMessageInfoFromValue(descendantEvent.properties?.info);
					const providerID = stringValue(info?.providerID);
					const modelID = stringValue(info?.modelID);
					if (providerID && modelID) modelId = `${providerID}/${modelID}`;
				}
				emitDescendantEvent(descendantEvent);
			};
			descendantEventProcessors.set(descendantSessionId, processEvent);
		}
		processEvent(event);
	};
	for await (const rawEvent of stream) {
		if (signal.aborted || turn.abortSignal.aborted) break;
		const event = unwrapOpenCodeEvent(rawEvent);
		const eventSessionId = event ? getOpenCodeEventSessionId(event) : void 0;
		if (!event) continue;
		const scopedSessionId = !eventSessionId || eventSessionId === sessionId ? sessionId : taskSessionIds.has(eventSessionId) ? eventSessionId : void 0;
		if (!scopedSessionId) continue;
		const isDescendant = scopedSessionId !== sessionId;
		if (event.type === "question.asked") await handleQuestion({
			client,
			turn,
			emit,
			event
		});
		else if (event.type === "permission.v2.asked") await handlePermissionV2({
			client,
			sessionId: scopedSessionId,
			permissionMode,
			builtinToolFiltering,
			turn,
			emit,
			event
		});
		else if (event.type === "permission.asked") await handlePermission({
			client,
			sessionId: scopedSessionId,
			permissionMode,
			builtinToolFiltering,
			turn,
			emit,
			event
		});
		else if (isDescendant) processDescendantEvent(scopedSessionId, event);
		else emitStreamEvent(event);
		if (isDescendant) continue;
		if (onEvent?.(event)) break;
	}
}
function getSubagentStepId(event) {
	if (event?.type === "message.part.updated") {
		const part = asOpenCodeObject(event.properties?.part);
		if (part?.type !== "step-finish") return void 0;
		return stringValue(part.id) ?? stringValue(part.messageID) ?? event.id;
	}
	if (event?.type !== "session.next.step.ended") return void 0;
	return stringValue(event.properties?.stepID) ?? event.id;
}
async function handleQuestion({ client, turn, emit, event }) {
	const nativeRequest = event.properties;
	if (nativeRequest == null || typeof nativeRequest.id !== "string" || typeof nativeRequest.sessionID !== "string" || !Array.isArray(nativeRequest.questions)) return;
	const toolCallId = nativeRequest.tool?.callID ?? nativeRequest.id;
	emit({
		type: "tool-call",
		toolCallId,
		toolName: "askUserQuestions",
		nativeName: "question",
		input: JSON.stringify(toHarnessQuestionsInput(nativeRequest)),
		providerExecuted: false,
		providerMetadata: { opencode: { nativeRequest } }
	});
	const questionKey = openCodeQuestionKey(nativeRequest);
	const nativeResponse = toOpenCodeQuestionResponse({
		nativeRequest,
		output: (await turn.requestToolResult({
			toolCallId,
			matches: (candidate) => {
				const continuedRequest = candidate.toolResult?.providerOptions?.opencode?.nativeRequest;
				return continuedRequest != null && openCodeQuestionKey(continuedRequest) === questionKey;
			}
		})).output
	});
	const response = nativeResponse.action === "reject" ? await client.question.reject({
		requestID: nativeRequest.id,
		directory: workdir
	}) : await client.question.reply({
		requestID: nativeRequest.id,
		directory: workdir,
		answers: nativeResponse.answers
	});
	if (response.error != null) throw new Error(`OpenCode question response failed: ${formatError(response.error)}`);
}
function sanitizeMcpToolName(value) {
	return value.replace(/[^a-zA-Z0-9_-]/g, "_");
}
async function handlePermissionV2({ client, sessionId, permissionMode, builtinToolFiltering, turn, emit, event }) {
	const props = event.properties ?? {};
	const requestID = String(props.id ?? "");
	if (!requestID) return;
	const reply = await selectPermissionReply({
		action: String(props.action ?? ""),
		resources: Array.isArray(props.resources) ? props.resources.map(String) : [],
		requestID,
		toolCallId: String(props.source?.callID ?? requestID),
		permissionMode,
		builtinToolFiltering,
		turn,
		emit
	});
	await client.v2.session.permission.reply({
		sessionID: sessionId,
		requestID,
		reply: reply.reply,
		...reply.message ? { message: reply.message } : {}
	});
}
async function handlePermission({ client, sessionId, permissionMode, builtinToolFiltering, turn, emit, event }) {
	const props = event.properties ?? {};
	const requestID = String(props.id ?? "");
	if (!requestID) return;
	const tool = asOpenCodeObject(props.tool);
	const reply = await selectPermissionReply({
		action: String(props.permission ?? ""),
		resources: Array.isArray(props.patterns) ? props.patterns.map(String) : [],
		requestID,
		toolCallId: String(tool?.callID ?? requestID),
		permissionMode,
		builtinToolFiltering,
		turn,
		emit
	});
	await client.permission.reply({
		requestID,
		directory: workdir,
		reply: reply.reply,
		...reply.message ? { message: reply.message } : {}
	});
}
async function selectPermissionReply({ action, resources, requestID, toolCallId, permissionMode, builtinToolFiltering, turn, emit }) {
	const toolName = toPermissionToolName(action);
	if (isBuiltinToolInactive({
		toolName,
		toolFiltering: builtinToolFiltering
	})) {
		emit({
			type: "tool-approval-request",
			approvalId: requestID,
			toolCallId
		});
		const decision = await turn.requestToolApproval(requestID);
		return decision.approved ? { reply: "once" } : {
			reply: "reject",
			...decision.reason ? { message: decision.reason } : {}
		};
	}
	if (!permissionMode || permissionMode === "allow-all") return { reply: "always" };
	if (resources.some((resource) => isExternalPath(resource))) return {
		reply: "reject",
		message: "External directory access rejected."
	};
	const kind = TOOL_KIND[toolName] ?? "bash";
	if (permissionMode === "allow-edits" ? kind === "readonly" || kind === "edit" : kind === "readonly") return { reply: "always" };
	emit({
		type: "tool-approval-request",
		approvalId: requestID,
		toolCallId
	});
	const decision = await turn.requestToolApproval(requestID);
	return decision.approved ? { reply: "once" } : {
		reply: "reject",
		...decision.reason ? { message: decision.reason } : {}
	};
}
function toPermissionToolName(action) {
	const normalized = action.toLowerCase();
	if (normalized.includes("bash") || normalized.includes("shell")) return "bash";
	if (normalized.includes("edit")) return "edit";
	if (normalized.includes("write")) return "write";
	if (normalized.includes("webfetch")) return "webfetch";
	if (normalized.includes("task") || normalized.includes("agent")) return "agent";
	if (normalized.includes("list")) return "ls";
	if (normalized.includes("grep")) return "grep";
	if (normalized.includes("glob")) return "glob";
	if (normalized.includes("read")) return "read";
	return toWireToolName(normalized);
}
function resolveInactiveBuiltinToolNames(start) {
	const toolFiltering = start.builtinToolFiltering;
	if (toolFiltering == null) return [];
	return toolFiltering.mode === "allow" ? Object.keys(PUBLIC_TO_NATIVE).filter((name) => !toolFiltering.toolNames.includes(name)) : toolFiltering.toolNames;
}
function isBuiltinToolInactive(input) {
	if (input.toolFiltering == null) return false;
	return input.toolFiltering.mode === "allow" ? !input.toolFiltering.toolNames.includes(input.toolName) : input.toolFiltering.toolNames.includes(input.toolName);
}
function isExternalPath(resource) {
	if (!path.isAbsolute(resource)) return false;
	return !isPathInsideOrEqual(resource, workdir) && (!skillsDir || !isPathInsideOrEqual(resource, skillsDir));
}
function isPathInsideOrEqual(file, root) {
	const relative = path.relative(canonicalizeForContainment(root), canonicalizeForContainment(file));
	return relative === "" || relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}
function canonicalizeForContainment(inputPath) {
	const normalized = path.resolve(inputPath);
	try {
		return realpathSync.native(normalized);
	} catch {
		const parent = path.dirname(normalized);
		return parent === normalized ? normalized : path.join(canonicalizeForContainment(parent), path.basename(normalized));
	}
}
function toWireToolName(nativeName) {
	return NATIVE_TO_COMMON[nativeName] ?? OPENCODE_TO_WIRE[nativeName] ?? nativeName;
}
function nativeNameField({ nativeName, toolName }) {
	if (!nativeName || nativeName === toolName || toolName === "agent") return {};
	return { nativeName };
}
function getHostToolName(toolName, rawToolName) {
	if (runtime.toolNames.has(toolName)) return toolName;
	if (typeof rawToolName === "string" && runtime.toolNames.has(rawToolName)) return rawToolName;
	if (typeof rawToolName === "string" && rawToolName.startsWith("harness-tools_") && runtime.toolNames.has(rawToolName.slice(14))) return rawToolName.slice(14);
}
function authorizeHostToolCall({ callID, toolName, input, state }) {
	if (state.hostToolCallsAuthorized.has(callID)) return;
	state.hostToolCallsAuthorized.add(callID);
	runtime.relay?.authorizeToolCall({
		toolName,
		input
	});
}
async function emitContextFallback({ client, sessionId, assistantBaseline, state, emit, emitContent }) {
	const assistant = await latestAssistantSnapshot({
		client,
		sessionId
	});
	if (!assistant || !isAssistantSnapshotAfterBaseline({
		assistant,
		baseline: assistantBaseline
	})) return false;
	emitOpenCodeStreamStart({
		info: assistant,
		state,
		emit
	});
	if (emitContent && Array.isArray(assistant.contentParts)) for (const part of assistant.contentParts) emitAssistantContentPart(part, emit);
	const rawFinish = typeof assistant.finish === "string" ? assistant.finish : assistant.error ? "error" : "stop";
	emit({
		type: "finish-step",
		finishReason: {
			unified: mapOpenCodeFinishReason(rawFinish),
			raw: rawFinish
		},
		usage: mapUsage(assistant.tokens),
		...typeof assistant.cost === "number" ? { harnessMetadata: { opencode: {
			cost: assistant.cost,
			fallback: true
		} } } : { harnessMetadata: { opencode: { fallback: true } } }
	});
	return true;
}
async function readSessionTokens({ client, sessionId }) {
	const session = await legacySessionGet({
		client,
		sessionId
	});
	if (session.error) return void 0;
	return extractSessionTokens(session.data);
}
async function latestAssistantSnapshot({ client, sessionId }) {
	const legacyAssistant = latestLegacyAssistantMessage((await client.session.messages({
		sessionID: sessionId,
		limit: 20
	}).catch(() => void 0))?.data);
	if (legacyAssistant) return legacyAssistant;
	const context = await client.v2.session.context({ sessionID: sessionId }).catch(() => void 0);
	if (!context || context.error) return void 0;
	return latestV2AssistantMessage(context.data);
}
function latestLegacyAssistantMessage(data) {
	const messages = Array.isArray(data) ? data : [];
	for (let i = messages.length - 1; i >= 0; i--) {
		const item = messages[i];
		if (!item || typeof item !== "object") continue;
		const record = item;
		const info = record.info;
		if (info && typeof info === "object" && info.role === "assistant") return {
			...info,
			contentParts: Array.isArray(record.parts) ? record.parts : void 0
		};
	}
}
function latestV2AssistantMessage(data) {
	const messages = data && typeof data === "object" && Array.isArray(data.data) ? data.data : Array.isArray(data) ? data : [];
	for (let i = messages.length - 1; i >= 0; i--) {
		const message = messages[i];
		if (message && typeof message === "object" && message.type === "assistant") {
			const record = message;
			return {
				...record,
				contentParts: Array.isArray(record.content) ? record.content : void 0
			};
		}
	}
}
function emitAssistantContentPart(part, emit) {
	if (!part || typeof part !== "object") return;
	const value = part;
	if (value.type !== "text" && value.type !== "reasoning") return;
	const id = typeof value.id === "string" && value.id.length > 0 ? value.id : `${value.type}-${randomUUID()}`;
	const text = typeof value.text === "string" ? value.text : "";
	if (value.type === "text") {
		emit({
			type: "text-start",
			id
		});
		if (text) emit({
			type: "text-delta",
			id,
			delta: text
		});
		emit({
			type: "text-end",
			id
		});
		return;
	}
	emit({
		type: "reasoning-start",
		id
	});
	if (text) emit({
		type: "reasoning-delta",
		id,
		delta: text
	});
	emit({
		type: "reasoning-end",
		id
	});
}
async function startToolRelay({ tools, emit, requestToolResult }) {
	return startAuthorizedToolRelay({
		tools,
		emit,
		requestToolResult
	});
}
function createDeferred() {
	let resolve;
	return {
		promise: new Promise((res) => {
			resolve = res;
		}),
		resolve
	};
}
function sleep(ms) {
	return new Promise((resolve) => setTimeout(resolve, ms));
}
function splitModel(model, provider) {
	if (!model) return {};
	if (model.includes("/")) {
		const [providerID, ...rest] = model.split("/");
		return {
			providerID,
			modelID: rest.join("/")
		};
	}
	return {
		providerID: provider,
		modelID: model
	};
}
async function resolveCompactionModel({ client, sessionId, start }) {
	const assistantModel = modelRefFromAssistantSnapshot(await latestAssistantSnapshot({
		client,
		sessionId
	}).catch(() => void 0));
	if (assistantModel) return assistantModel;
	const sessionModel = modelRefFromSessionInfo((await legacySessionGet({
		client,
		sessionId
	}).catch(() => void 0))?.data);
	if (sessionModel) return sessionModel;
	return modelRefFromStart(start);
}
function modelRefFromAssistantSnapshot(assistant) {
	if (!assistant) return void 0;
	const model = modelRefFromValue(assistant.model);
	if (model) return model;
	const direct = modelRefFromValue(assistant);
	if (direct) return direct;
	return modelRefFromValue(asOpenCodeObject(assistant.metadata)?.assistant);
}
function modelRefFromSessionInfo(data) {
	const session = asOpenCodeObject(data);
	if (!session) return void 0;
	return modelRefFromValue(session.model) ?? modelRefFromObject(session);
}
function modelRefFromStart(start) {
	const model = splitModel(start.model, start.provider);
	if (!model.modelID) return void 0;
	return {
		providerID: model.providerID ?? start.provider ?? env.OPENAI_NAME ?? "anthropic",
		modelID: model.modelID
	};
}
function modelRefFromValue(value) {
	const model = asOpenCodeObject(value);
	return model ? modelRefFromObject(model) : void 0;
}
function modelRefFromObject(value) {
	const providerID = stringValue(value.providerID);
	const modelID = stringValue(value.modelID ?? value.id);
	if (!providerID || !modelID) return void 0;
	return {
		providerID,
		modelID
	};
}
function stripWorkDir(file) {
	if (!file) return file;
	const normalized = path.resolve(file);
	const root = path.resolve(workdir);
	return normalized.startsWith(`${root}/`) ? normalized.slice(root.length + 1) : file;
}
function parseArgs(args) {
	const out = {};
	for (let i = 0; i < args.length; i++) if (args[i] === "--workdir" && i + 1 < args.length) out.workdir = args[++i];
	else if (args[i] === "--bridge-state-dir" && i + 1 < args.length) out.bridgeStateDir = args[++i];
	else if (args[i] === "--bootstrap-dir" && i + 1 < args.length) out.bootstrapDir = args[++i];
	else if (args[i] === "--skills-dir" && i + 1 < args.length) out.skillsDir = args[++i];
	return out;
}
function formatError(error) {
	if (error instanceof Error) {
		const cause = "cause" in error ? error.cause : void 0;
		if (cause === void 0) return error.message;
		return `${error.message}: ${formatError(cause)}`;
	}
	if (typeof error === "string") return error;
	try {
		return JSON.stringify(error);
	} catch {
		return String(error);
	}
}
function emitFatal(message) {
	process.stderr.write(`[OpenCode bridge] ${message}\n`);
	process.exit(1);
}
//#endregion
export {};

//# sourceMappingURL=index.mjs.map