import { appendFile, mkdir, writeFile } from "node:fs/promises";
import { existsSync, readFileSync } from "node:fs";
import { createHash, randomUUID } from "node:crypto";
import { argv, env, pid, stdout } from "node:process";
import { WebSocketServer } from "ws";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { isDeepStrictEqual } from "node:util";
import path from "node:path";
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
//#region src/bridge/codex-tool-filtering.ts
function resolveCodexBuiltinToolPolicy({ builtinToolFiltering, webSearch }) {
	const included = (toolName) => builtinToolFiltering == null || (builtinToolFiltering.mode === "allow" ? builtinToolFiltering.toolNames.includes(toolName) : !builtinToolFiltering.toolNames.includes(toolName));
	const disabled = {
		bash: !included("bash"),
		webSearch: !included("webSearch"),
		apply_patch: !included("apply_patch"),
		view_image: !included("view_image")
	};
	const disableEnvironments = disabled.bash && disabled.apply_patch && disabled.view_image;
	return {
		disabled,
		disableEnvironments,
		denyApplyPatchWithHook: disabled.apply_patch && !disableEnvironments,
		webSearchMode: webSearch && !disabled.webSearch ? "live" : "disabled"
	};
}
//#endregion
//#region src/bridge/codex-app-server-client.ts
var CodexAppServerRequestError = class extends Error {
	constructor({ method, error }) {
		super(`Codex app-server ${method} failed: ${error.message}`);
		this.name = "CodexAppServerRequestError";
		this.method = method;
		this.code = error.code;
		this.data = error.data;
	}
};
var CodexAppServerClient = class {
	constructor({ executable, args, cwd, env, onNotification, onRequest, onStderr }) {
		this.pending = /* @__PURE__ */ new Map();
		this.nextRequestId = 1;
		this.stdoutBuffer = "";
		this.parseQueue = Promise.resolve();
		this.closed = false;
		this.resolveFailure = () => {};
		this.exitResult = null;
		this.onNotification = onNotification;
		this.onRequest = onRequest;
		this.onStderr = onStderr;
		this.failed = new Promise((resolve) => {
			this.resolveFailure = resolve;
		});
		this.child = spawn(executable, args, {
			cwd,
			env,
			stdio: [
				"pipe",
				"pipe",
				"pipe"
			]
		});
		this.exited = new Promise((resolve) => {
			this.child.once("exit", (code, signal) => {
				this.exitResult = {
					code,
					signal
				};
				this.fail(/* @__PURE__ */ new Error(`Codex app-server exited (code ${code ?? "null"}, signal ${signal ?? "null"}).`));
				resolve(this.exitResult);
			});
		});
		this.child.once("error", (error) => this.fail(error));
		this.child.stdout.setEncoding("utf8");
		this.child.stdout.on("data", (chunk) => this.consumeStdout(String(chunk)));
		this.child.stderr.setEncoding("utf8");
		this.child.stderr.on("data", (chunk) => this.onStderr(String(chunk)));
	}
	async initialize({ clientName, clientVersion }) {
		await this.request({
			method: "initialize",
			params: {
				clientInfo: {
					name: clientName,
					version: clientVersion
				},
				capabilities: { experimentalApi: true }
			}
		});
		this.notify({ method: "initialized" });
	}
	request({ method, params }) {
		if (this.closed || this.exitResult != null) return Promise.reject(/* @__PURE__ */ new Error("Codex app-server is not running."));
		const id = this.nextRequestId++;
		const response = new Promise((resolve, reject) => {
			this.pending.set(id, {
				method,
				resolve,
				reject
			});
		});
		try {
			this.write({
				id,
				method,
				...params === void 0 ? {} : { params }
			});
		} catch (error) {
			this.pending.delete(id);
			return Promise.reject(error);
		}
		return response;
	}
	notify({ method, params }) {
		this.write({
			method,
			...params === void 0 ? {} : { params }
		});
	}
	async close() {
		if (this.closed) return;
		this.closed = true;
		this.child.stdin.end();
		if (await this.waitForExit({ timeoutMs: 500 })) return;
		this.child.kill("SIGTERM");
		if (await this.waitForExit({ timeoutMs: 1e3 })) return;
		this.child.kill("SIGKILL");
		await this.waitForExit({ timeoutMs: 1e3 });
	}
	waitUntilExit() {
		return this.exited;
	}
	waitUntilFailure() {
		return this.failed.then((error) => {
			throw error;
		});
	}
	write(message) {
		if (this.closed || this.exitResult != null || !this.child.stdin.writable) throw new Error("Codex app-server is not writable.");
		this.child.stdin.write(`${JSON.stringify(message)}\n`);
	}
	consumeStdout(chunk) {
		this.stdoutBuffer += chunk;
		let newline = this.stdoutBuffer.indexOf("\n");
		while (newline !== -1) {
			const line = this.stdoutBuffer.slice(0, newline).trim();
			this.stdoutBuffer = this.stdoutBuffer.slice(newline + 1);
			if (line.length > 0) this.parseQueue = this.parseQueue.then(() => this.processLine(line)).catch((error) => this.fail(error));
			newline = this.stdoutBuffer.indexOf("\n");
		}
	}
	async processLine(line) {
		let message;
		try {
			message = JSON.parse(line);
		} catch {
			throw new Error("Codex app-server wrote an invalid JSON-RPC message.");
		}
		if (!isRecord(message)) throw new Error("Codex app-server wrote an invalid JSON-RPC message.");
		if (typeof message.method === "string") {
			if (isJsonRpcId(message.id)) this.handleServerRequest({
				id: message.id,
				method: message.method,
				...message.params === void 0 ? {} : { params: message.params }
			});
			else this.onNotification({
				method: message.method,
				...message.params === void 0 ? {} : { params: message.params }
			});
			return;
		}
		if (!isJsonRpcId(message.id)) throw new Error("Codex app-server response is missing a valid id.");
		const pending = this.pending.get(message.id);
		if (pending == null) return;
		this.pending.delete(message.id);
		if (isJsonRpcError(message.error)) {
			pending.reject(new CodexAppServerRequestError({
				method: pending.method,
				error: message.error
			}));
			return;
		}
		if (!Object.prototype.hasOwnProperty.call(message, "result")) {
			pending.reject(/* @__PURE__ */ new Error(`Codex app-server ${pending.method} response has no result.`));
			return;
		}
		pending.resolve(message.result);
	}
	async handleServerRequest(request) {
		try {
			const result = await this.onRequest(request);
			if (!this.closed && this.exitResult == null) this.write({
				id: request.id,
				result
			});
		} catch (error) {
			if (!this.closed && this.exitResult == null) this.write({
				id: request.id,
				error: {
					code: -32603,
					message: error instanceof Error ? error.message : String(error)
				}
			});
		}
	}
	failPending(error) {
		for (const pending of this.pending.values()) pending.reject(error);
		this.pending.clear();
	}
	fail(error) {
		this.failPending(error);
		if (this.failure !== void 0) return;
		this.failure = error;
		this.resolveFailure(error);
	}
	async waitForExit({ timeoutMs }) {
		if (this.exitResult != null) return true;
		return Promise.race([this.exited.then(() => true), new Promise((resolve) => {
			setTimeout(() => resolve(false), timeoutMs).unref?.();
		})]);
	}
};
function isRecord(value) {
	return value != null && typeof value === "object" && !Array.isArray(value);
}
function isJsonRpcId(value) {
	return typeof value === "string" || typeof value === "number";
}
function isJsonRpcError(value) {
	return isRecord(value) && typeof value.code === "number" && typeof value.message === "string";
}
//#endregion
//#region src/bridge/create-app-server-event-handler.ts
function createAppServerEventHandler({ stepTracker, emitStreamEvent, emitWarning, emitError }) {
	let activeThreadId;
	let activeTurnId;
	let settled = false;
	let accumulatedUsage = emptyUsageBreakdown();
	let lastCumulativeUsageKey;
	const textByItem = /* @__PURE__ */ new Map();
	const reasoningByItem = /* @__PURE__ */ new Map();
	const nativeToolCalls = /* @__PURE__ */ new Map();
	let resolveCompletion = () => {};
	const completion = new Promise((resolve) => {
		resolveCompletion = resolve;
	});
	const announceThread = (threadId) => {
		if (activeThreadId != null) return;
		activeThreadId = threadId;
		emitStreamEvent({
			type: "thread.started",
			thread_id: threadId
		});
	};
	const handleItem = ({ eventType, params }) => {
		if (!matchesActiveTurn({
			params,
			activeThreadId,
			activeTurnId
		})) return;
		const item = asRecord$2(params.item);
		if (item == null || typeof item.type !== "string") return;
		if (eventType === "item.completed" && typeof item.id === "string") {
			const call = nativeToolCalls.get(item.id);
			if (call?.name === "apply_patch" && item.type === "fileChange") call.successConfirmed = item.status === "completed";
			else if (call?.name === "view_image" && item.type === "imageView") call.successConfirmed = true;
		}
		const normalized = normalizeItem({
			item,
			textByItem,
			reasoningByItem
		});
		if (normalized == null) return;
		if (normalized.type === "dynamic_tool_call") {
			stepTracker.observeEvent({
				event: {
					type: eventType,
					item: normalized
				},
				itemId: normalized.id
			});
			return;
		}
		emitStreamEvent({
			type: eventType,
			item: normalized
		});
	};
	const emitNativeToolResult = ({ callId, result }) => {
		const call = nativeToolCalls.get(callId);
		if (call == null || call.resultEmitted) return;
		call.resultEmitted = true;
		emitStreamEvent({
			type: "item.completed",
			item: {
				type: "native_tool",
				id: callId,
				tool: call.name,
				result,
				...!call.successConfirmed ? { isError: true } : {}
			}
		});
	};
	const handleRawItem = (params) => {
		if (!matchesActiveTurn({
			params,
			activeThreadId,
			activeTurnId
		})) return;
		const item = asRecord$2(params.item);
		if (item == null) return;
		const toolCall = normalizeNativeToolCall({ item });
		if (toolCall != null) {
			if (nativeToolCalls.has(toolCall.callId)) return;
			nativeToolCalls.set(toolCall.callId, {
				name: toolCall.name,
				resultEmitted: false,
				successConfirmed: false
			});
			emitStreamEvent({
				type: "item.started",
				item: {
					type: "native_tool",
					id: toolCall.callId,
					tool: toolCall.name,
					input: toolCall.input
				}
			});
			return;
		}
		if (typeof item.call_id !== "string") return;
		const call = nativeToolCalls.get(item.call_id);
		if (call == null || (call.name === "apply_patch" ? item.type !== "custom_tool_call_output" : item.type !== "function_call_output") || !Object.prototype.hasOwnProperty.call(item, "output")) return;
		emitNativeToolResult({
			callId: item.call_id,
			result: call.name === "view_image" && call.successConfirmed && Array.isArray(item.output) && item.output.length === 0 ? "Image viewed." : item.output
		});
	};
	return {
		announceThread,
		setTurnId(turnId) {
			activeTurnId = turnId;
		},
		handle(notification) {
			const params = asRecord$2(notification.params);
			if (notification.method === "thread/started") {
				const thread = asRecord$2(params?.thread);
				if (typeof thread?.id === "string") announceThread(thread.id);
				return;
			}
			if (notification.method === "turn/started") {
				const turn = asRecord$2(params?.turn);
				if (params?.threadId === activeThreadId && typeof turn?.id === "string") activeTurnId = turn.id;
				return;
			}
			if (notification.method === "item/started" && params != null) {
				handleItem({
					eventType: "item.started",
					params
				});
				return;
			}
			if (notification.method === "item/completed" && params != null) {
				handleItem({
					eventType: "item.completed",
					params
				});
				return;
			}
			if (notification.method === "rawResponseItem/completed" && params != null) {
				handleRawItem(params);
				return;
			}
			if (notification.method === "item/agentMessage/delta" && params != null && matchesActiveTurn({
				params,
				activeThreadId,
				activeTurnId
			}) && typeof params.itemId === "string" && typeof params.delta === "string") {
				const text = (textByItem.get(params.itemId) ?? "") + params.delta;
				textByItem.set(params.itemId, text);
				emitStreamEvent({
					type: "item.updated",
					item: {
						type: "agent_message",
						id: params.itemId,
						text
					}
				});
				return;
			}
			if (notification.method === "item/reasoning/summaryTextDelta" && params != null && matchesActiveTurn({
				params,
				activeThreadId,
				activeTurnId
			}) && typeof params.itemId === "string" && typeof params.delta === "string") {
				const text = (reasoningByItem.get(params.itemId) ?? "") + params.delta;
				reasoningByItem.set(params.itemId, text);
				emitStreamEvent({
					type: "item.updated",
					item: {
						type: "reasoning",
						id: params.itemId,
						text
					}
				});
				return;
			}
			if (notification.method === "thread/tokenUsage/updated" && params != null && matchesActiveTurn({
				params,
				activeThreadId,
				activeTurnId
			})) {
				const tokenUsage = asRecord$2(params.tokenUsage);
				const total = readUsageBreakdown(tokenUsage?.total);
				const last = readUsageBreakdown(tokenUsage?.last);
				if (total != null && last != null) {
					const key = usageKey(total);
					if (key !== lastCumulativeUsageKey) {
						lastCumulativeUsageKey = key;
						accumulatedUsage = addUsage({
							total: accumulatedUsage,
							increment: last
						});
					}
				}
				return;
			}
			if (notification.method === "error" && params != null) {
				if (!matchesActiveTurn({
					params,
					activeThreadId,
					activeTurnId
				})) return;
				const error = asRecord$2(params.error);
				const message = typeof error?.message === "string" ? error.message : "Codex app-server reported an error.";
				if (params.willRetry === true) emitWarning({ message });
				else emitError({
					error: message,
					message: "codex turn failed"
				});
				return;
			}
			if ((notification.method === "warning" || notification.method === "configWarning" || notification.method === "deprecationNotice") && typeof params?.message === "string") {
				emitWarning({ message: params.message });
				return;
			}
			if (notification.method === "turn/completed" && params != null && matchesActiveTurn({
				params,
				activeThreadId,
				activeTurnId
			})) {
				if (settled) return;
				settled = true;
				const turn = asRecord$2(params.turn);
				const status = typeof turn?.status === "string" ? turn.status : "failed";
				const turnError = asRecord$2(turn?.error);
				if (status === "completed") for (const [callId, call] of nativeToolCalls) {
					if (call.resultEmitted) continue;
					emitNativeToolResult({
						callId,
						result: call.successConfirmed ? call.name === "apply_patch" ? "Patch applied." : "Image viewed." : "Codex did not report the tool result."
					});
				}
				emitStreamEvent({
					type: "turn.completed",
					usage: toLegacyUsage(accumulatedUsage)
				});
				resolveCompletion({
					status,
					...typeof turnError?.message === "string" ? { error: turnError.message } : {}
				});
			}
		},
		waitForCompletion: () => completion
	};
}
function normalizeNativeToolCall({ item }) {
	if (item.namespace != null || typeof item.call_id !== "string" || item.call_id.length === 0) return;
	if (item.type === "custom_tool_call" && item.name === "apply_patch" && typeof item.input === "string") return {
		callId: item.call_id,
		name: "apply_patch",
		input: JSON.stringify(item.input)
	};
	if (item.type === "function_call" && item.name === "view_image" && typeof item.arguments === "string") return {
		callId: item.call_id,
		name: "view_image",
		input: item.arguments
	};
}
function normalizeItem({ item, textByItem, reasoningByItem }) {
	const id = typeof item.id === "string" ? item.id : void 0;
	if (item.type === "agentMessage") {
		const text = typeof item.text === "string" ? item.text : "";
		if (id != null) textByItem.set(id, text);
		return {
			type: "agent_message",
			id,
			text
		};
	}
	if (item.type === "reasoning") {
		const summary = stringArray(item.summary).join("\n\n");
		const content = stringArray(item.content).join("\n\n");
		const text = summary || reasoningByItem.get(id ?? "") || content;
		if (id != null) reasoningByItem.set(id, text);
		return {
			type: "reasoning",
			id,
			text
		};
	}
	if (item.type === "commandExecution") return {
		type: "command_execution",
		id,
		command: typeof item.command === "string" ? item.command : "",
		exit_code: typeof item.exitCode === "number" ? item.exitCode : void 0,
		aggregated_output: typeof item.aggregatedOutput === "string" ? item.aggregatedOutput : void 0,
		status: normalizeCommandStatus(item.status)
	};
	if (item.type === "mcpToolCall") {
		const result = asRecord$2(item.result);
		return {
			type: "mcp_tool_call",
			id,
			server: typeof item.server === "string" ? item.server : void 0,
			tool: typeof item.tool === "string" ? item.tool : void 0,
			arguments: item.arguments,
			result: result == null ? item.result : {
				content: result.content,
				structured_content: result.structuredContent
			},
			error: asRecord$2(item.error)
		};
	}
	if (item.type === "dynamicToolCall") return {
		type: "dynamic_tool_call",
		id
	};
	if (item.type === "webSearch") return {
		type: "web_search",
		id,
		query: typeof item.query === "string" ? item.query : void 0,
		action: asRecord$2(item.action) ?? void 0,
		result: item.results
	};
	if (item.type === "fileChange") return {
		type: "file_change",
		id,
		changes: Array.isArray(item.changes) ? item.changes.flatMap((change) => {
			const value = asRecord$2(change);
			const kind = asRecord$2(value?.kind)?.type;
			return typeof value?.path === "string" && (kind === "add" || kind === "delete" || kind === "update") ? [{
				path: value.path,
				kind
			}] : [];
		}) : []
	};
	if (item.type === "plan") return {
		type: "todo_list",
		id
	};
}
function normalizeCommandStatus(value) {
	if (value === "completed") return "completed";
	if (value === "inProgress") return "in_progress";
	return "failed";
}
function matchesActiveTurn({ params, activeThreadId, activeTurnId }) {
	const nestedTurn = asRecord$2(params.turn);
	const turnId = typeof params.turnId === "string" ? params.turnId : typeof nestedTurn?.id === "string" ? nestedTurn.id : void 0;
	return activeThreadId != null && activeTurnId != null && params.threadId === activeThreadId && turnId === activeTurnId;
}
function asRecord$2(value) {
	return value != null && typeof value === "object" && !Array.isArray(value) ? value : void 0;
}
function stringArray(value) {
	return Array.isArray(value) ? value.filter((entry) => typeof entry === "string") : [];
}
function emptyUsageBreakdown() {
	return {
		inputTokens: 0,
		cachedInputTokens: 0,
		cacheWriteInputTokens: 0,
		outputTokens: 0
	};
}
function readUsageBreakdown(value) {
	const usage = asRecord$2(value);
	if (usage == null) return void 0;
	return {
		inputTokens: numberOrZero(usage.inputTokens),
		cachedInputTokens: numberOrZero(usage.cachedInputTokens),
		cacheWriteInputTokens: numberOrZero(usage.cacheWriteInputTokens),
		outputTokens: numberOrZero(usage.outputTokens)
	};
}
function addUsage({ total, increment }) {
	return {
		inputTokens: total.inputTokens + increment.inputTokens,
		cachedInputTokens: total.cachedInputTokens + increment.cachedInputTokens,
		cacheWriteInputTokens: total.cacheWriteInputTokens + increment.cacheWriteInputTokens,
		outputTokens: total.outputTokens + increment.outputTokens
	};
}
function usageKey(usage) {
	return [
		usage.inputTokens,
		usage.cachedInputTokens,
		usage.cacheWriteInputTokens,
		usage.outputTokens
	].join(":");
}
function toLegacyUsage(usage) {
	return {
		input_tokens: usage.inputTokens,
		cached_input_tokens: usage.cachedInputTokens,
		cache_write_input_tokens: usage.cacheWriteInputTokens,
		output_tokens: usage.outputTokens
	};
}
function numberOrZero(value) {
	return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
//#endregion
//#region src/bridge/codex-tool-filtering-hook.ts
const MATCHER = "^apply_patch$";
const TIMEOUT_SECONDS = 10;
const COMMAND = "printf '%s\\n' \"Tool 'apply_patch' is inactive due to the HarnessAgent tool filtering policy.\" >&2; exit 2";
function createTrustedApplyPatchHook({ codexConfig }) {
	const threadConfig = { ...codexConfig };
	const hooks = { ...asRecord$1(threadConfig.hooks) };
	delete threadConfig.hooks;
	for (const [key, value] of Object.entries(threadConfig)) {
		if (!key.startsWith("hooks.")) continue;
		const name = key.slice(6);
		if (name.startsWith("state.")) hooks.state = {
			...asRecord$1(hooks.state),
			[name.slice(6)]: value
		};
		else hooks[name] = value;
		delete threadConfig[key];
	}
	const userGroups = hooks.PreToolUse ?? [];
	if (!Array.isArray(userGroups)) throw new Error("Codex config hooks.PreToolUse must be an array.");
	const handler = {
		type: "command",
		command: COMMAND,
		timeout: TIMEOUT_SECONDS,
		async: false
	};
	const hash = `sha256:${createHash("sha256").update(JSON.stringify(canonicalize({
		event_name: "pre_tool_use",
		matcher: MATCHER,
		hooks: [handler]
	}))).digest("hex")}`;
	const key = `${process.platform === "win32" ? path.win32.resolve("C:\\", "<session-flags>/config.toml") : path.posix.resolve("/", "<session-flags>/config.toml")}:pre_tool_use:${userGroups.length}:0`;
	hooks.PreToolUse = [...userGroups, {
		matcher: MATCHER,
		hooks: [handler]
	}];
	hooks.state = {
		...asRecord$1(hooks.state),
		[key]: {
			enabled: true,
			trusted_hash: hash
		}
	};
	return {
		cliOverrides: [`hooks=${toTomlInline(hooks)}`, "features.hooks=true"],
		threadConfig,
		key,
		hash,
		command: COMMAND
	};
}
function assertTrustedApplyPatchHook({ response, hook }) {
	const data = asRecord$1(response)?.data;
	const record = asRecord$1(Array.isArray(data) && data.length === 1 ? data[0] : null);
	const matches = Array.isArray(record?.hooks) ? record.hooks.filter((item) => asRecord$1(item)?.key === hook.key) : [];
	const match = matches.length === 1 ? asRecord$1(matches[0]) : void 0;
	if (!match || match.eventName !== "preToolUse" || match.handlerType !== "command" || match.command !== hook.command || match.matcher !== MATCHER || match.currentHash !== hook.hash || match.enabled !== true || match.trustStatus !== "trusted" || Array.isArray(record?.errors) && record.errors.length > 0) throw new Error("Codex app-server did not load the trusted apply_patch filtering hook.");
}
function canonicalize(value) {
	if (Array.isArray(value)) return value.map(canonicalize);
	if (value != null && typeof value === "object") return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => [key, canonicalize(item)]));
	return value;
}
function toTomlInline(value) {
	if (typeof value === "string") return JSON.stringify(value);
	if (typeof value === "boolean") return String(value);
	if (typeof value === "number" && Number.isFinite(value)) return String(value);
	if (Array.isArray(value)) return `[${value.map(toTomlInline).join(",")}]`;
	if (value != null && typeof value === "object") return `{${Object.entries(value).filter(([, item]) => item !== void 0).map(([key, item]) => `${JSON.stringify(key)}=${toTomlInline(item)}`).join(",")}}`;
	throw new Error("Codex hook configuration contains an unsupported value.");
}
function asRecord$1(value) {
	return value != null && typeof value === "object" && !Array.isArray(value) ? value : void 0;
}
//#endregion
//#region src/bridge/codex-app-server-driver.ts
const CODEX_CLI_PATH = fileURLToPath(new URL("./node_modules/@openai/codex/bin/codex.js", import.meta.url));
function createCodexAppServerRuntime() {
	let client;
	let loadedThreadId;
	let loadedConfig;
	let activeTurn;
	const close = async () => {
		const previous = client;
		client = void 0;
		loadedThreadId = void 0;
		loadedConfig = void 0;
		await previous?.close();
	};
	const runTurn = async (options) => {
		const { start, turn, emit, workdir, threadId, codexModel, codexConfig } = options;
		if (activeTurn != null) throw new Error("A Codex turn is already active.");
		const toolPolicy = resolveCodexBuiltinToolPolicy({
			builtinToolFiltering: start.builtinToolFiltering,
			webSearch: start.webSearch
		});
		const trustedHook = toolPolicy.denyApplyPatchWithHook ? createTrustedApplyPatchHook({ codexConfig }) : void 0;
		const nextConfig = {
			...trustedHook?.threadConfig ?? codexConfig,
			web_search: toolPolicy.webSearchMode,
			...toolPolicy.disabled.bash || toolPolicy.disabled.view_image ? { features: {
				...asRecord(codexConfig.features),
				...toolPolicy.disabled.bash ? { shell_tool: false } : {},
				...toolPolicy.disabled.view_image ? { view_image: false } : {}
			} } : {}
		};
		const processConfig = {
			thread: nextConfig,
			cliOverrides: trustedHook?.cliOverrides ?? []
		};
		if (client != null && !isDeepStrictEqual(loadedConfig, processConfig)) await close();
		const handler = createAppServerEventHandler({
			stepTracker: options.stepTracker,
			emitStreamEvent: options.emitStreamEvent,
			emitWarning: turn.emitWarning,
			emitError: turn.emitError
		});
		const dynamicTools = createDynamicTools(start.tools ?? []);
		let rejectProtocolFailure = () => {};
		const protocolFailure = new Promise((_, reject) => {
			rejectProtocolFailure = reject;
		});
		const currentTurn = {
			threadId,
			turnId: void 0,
			handler,
			dynamicTools,
			options,
			fail: rejectProtocolFailure
		};
		activeTurn = currentTurn;
		let initialized = false;
		if (client == null) {
			const created = new CodexAppServerClient({
				executable: process.execPath,
				args: createCodexAppServerArgs({ cliOverrides: trustedHook?.cliOverrides }),
				cwd: workdir,
				env: process.env,
				onNotification: (notification) => {
					if (client !== created || activeTurn == null) return;
					const params = asRecord(notification.params);
					if (notification.method === "turn/started" && params?.threadId === activeTurn.threadId && activeTurn.turnId == null) {
						const turnId = asRecord(params?.turn)?.id;
						if (typeof turnId === "string") activeTurn.turnId = turnId;
					}
					activeTurn.handler.handle(notification);
				},
				onRequest: (request) => {
					const active = activeTurn;
					const params = asRecord(request.params);
					if (client !== created || active == null || params?.threadId !== active.threadId || params?.turnId !== active.turnId || active.turnId == null) return Promise.reject(/* @__PURE__ */ new Error("Codex app-server requested a tool for an inactive turn."));
					return handleAppServerRequest({
						request,
						dynamicTools: active.dynamicTools,
						emit: active.options.emit,
						requestToolResult: active.options.turn.requestToolResult
					}).catch((error) => {
						active.fail(error);
						throw error;
					});
				},
				onStderr: (text) => {
					const message = text.trim();
					if (message.length > 0) activeTurn?.options.turn.bridgeLog({
						level: "debug",
						subsystem: "codex.app-server.stderr",
						message
					});
				}
			});
			client = created;
			loadedConfig = processConfig;
			initialized = true;
		}
		const runningClient = client;
		const clientFailure = runningClient.waitUntilFailure();
		const exitFailure = runningClient.waitUntilExit().then(({ code, signal }) => {
			throw new Error(`Codex app-server exited before the turn completed (code ${code ?? "null"}, signal ${signal ?? "null"}).`);
		});
		let removeAbortListener = () => {};
		const abortFailure = new Promise((_, reject) => {
			const onAbort = () => {
				if (currentTurn.threadId != null && currentTurn.turnId != null) runningClient.request({
					method: "turn/interrupt",
					params: {
						threadId: currentTurn.threadId,
						turnId: currentTurn.turnId
					}
				}).catch(() => {});
				reject(turn.abortSignal.reason ?? new DOMException("Aborted", "AbortError"));
			};
			if (turn.abortSignal.aborted) onAbort();
			else {
				turn.abortSignal.addEventListener("abort", onAbort, { once: true });
				removeAbortListener = () => turn.abortSignal.removeEventListener("abort", onAbort);
			}
		});
		const raceWithProcess = ({ operation }) => Promise.race([
			operation,
			abortFailure,
			protocolFailure,
			clientFailure,
			exitFailure
		]);
		let stopSteering = () => {};
		const steeringStopped = new Promise((resolve) => {
			stopSteering = resolve;
		});
		const userMessageLoop = async () => {
			for await (const message of turn.experimental_userMessages) try {
				if (asRecord(await Promise.race([raceWithProcess({ operation: runningClient.request({
					method: "turn/steer",
					params: {
						threadId: currentTurn.threadId,
						expectedTurnId: currentTurn.turnId,
						clientUserMessageId: message.messageId,
						input: [{
							type: "text",
							text: message.text,
							text_elements: []
						}]
					}
				}) }), steeringStopped.then(() => {
					throw new Error("The Codex turn ended before accepting the user message.");
				})]))?.turnId !== currentTurn.turnId) throw new Error("Codex app-server turn/steer returned an unexpected turn ID.");
				message.accept();
			} catch (error) {
				message.reject(error);
			}
		};
		let keepClient = false;
		try {
			if (initialized) await raceWithProcess({ operation: runningClient.initialize({
				clientName: "ai-sdk-harness-codex",
				clientVersion: "1"
			}) });
			if (start.restartThread && loadedThreadId != null) {
				await raceWithProcess({ operation: runningClient.request({
					method: "thread/unsubscribe",
					params: { threadId: loadedThreadId }
				}) });
				loadedThreadId = void 0;
			}
			if (trustedHook != null) assertTrustedApplyPatchHook({
				response: await raceWithProcess({ operation: runningClient.request({
					method: "hooks/list",
					params: {}
				}) }),
				hook: trustedHook
			});
			const threadMethod = threadId == null ? "thread/start" : "thread/resume";
			if (loadedThreadId !== threadId || threadId == null) {
				const threadResponse = await raceWithProcess({ operation: runningClient.request({
					method: threadMethod,
					params: threadId == null ? {
						...createThreadParams({
							start,
							workdir,
							codexModel,
							codexConfig: nextConfig
						}),
						dynamicTools: dynamicTools.specs,
						experimentalRawEvents: true
					} : {
						threadId,
						...createThreadParams({
							start,
							workdir,
							codexModel,
							codexConfig: nextConfig
						}),
						excludeTurns: true
					}
				}) });
				assertCodexThreadPermissions({
					response: threadResponse,
					method: threadMethod
				});
				currentTurn.threadId = readNestedString({
					value: threadResponse,
					path: ["thread", "id"],
					method: threadMethod
				});
				loadedThreadId = currentTurn.threadId;
			}
			handler.announceThread(currentTurn.threadId);
			emit({ type: "stream-start" });
			currentTurn.turnId = readNestedString({
				value: await raceWithProcess({ operation: runningClient.request({
					method: "turn/start",
					params: createTurnParams({
						threadId: currentTurn.threadId,
						start,
						codexModel,
						disableEnvironments: toolPolicy.disableEnvironments
					})
				}) }),
				path: ["turn", "id"],
				method: "turn/start"
			});
			handler.setTurnId(currentTurn.turnId);
			userMessageLoop();
			const result = await raceWithProcess({ operation: handler.waitForCompletion() });
			keepClient = true;
			assertSuccessfulTurn(result);
		} catch (error) {
			if (turn.abortSignal.aborted && currentTurn.turnId != null) {
				let timer;
				try {
					keepClient = await Promise.race([
						handler.waitForCompletion().then(() => true),
						new Promise((resolve) => {
							timer = setTimeout(() => resolve(false), 5e3);
						}),
						clientFailure.then(() => false, () => false)
					]);
				} finally {
					clearTimeout(timer);
				}
			}
			throw error;
		} finally {
			turn.experimental_userMessages.close();
			stopSteering();
			removeAbortListener();
			if (activeTurn === currentTurn) activeTurn = void 0;
			if (!keepClient) await close();
		}
	};
	return {
		runTurn,
		close
	};
}
function createCodexAppServerArgs({ cliOverrides = [] } = {}) {
	return [
		CODEX_CLI_PATH,
		"--config",
		"sandbox_mode=\"danger-full-access\"",
		"--config",
		"approval_policy=\"never\"",
		...cliOverrides.flatMap((override) => ["--config", override]),
		"app-server",
		"--stdio"
	];
}
function createThreadParams({ start, workdir, codexModel, codexConfig }) {
	return {
		...codexModel == null ? {} : { model: codexModel },
		cwd: workdir,
		approvalPolicy: "never",
		sandbox: "danger-full-access",
		config: {
			...codexConfig,
			web_search: codexConfig.web_search ?? (start.webSearch ? "live" : "disabled")
		}
	};
}
function createTurnParams({ threadId, start, codexModel, disableEnvironments = false }) {
	return {
		threadId,
		...disableEnvironments ? { environments: [] } : {},
		input: [{
			type: "text",
			text: start.prompt,
			text_elements: []
		}],
		approvalPolicy: "never",
		sandboxPolicy: {
			type: "externalSandbox",
			networkAccess: "enabled"
		},
		...codexModel == null ? {} : { model: codexModel },
		...start.reasoningEffort == null ? {} : { effort: start.reasoningEffort },
		...start.responseFormat?.type === "json" && start.responseFormat.schema != null ? { outputSchema: start.responseFormat.schema } : {}
	};
}
function assertCodexThreadPermissions({ response, method }) {
	const value = asRecord(response);
	const sandbox = asRecord(value?.sandbox);
	if (value?.approvalPolicy !== "never" || sandbox?.type !== "dangerFullAccess") throw new Error(`Codex app-server ${method} did not disable approvals and its platform sandbox.`);
}
function createDynamicTools(tools) {
	const validOriginalNames = new Set(tools.map((tool) => tool.name).filter(isValidDynamicToolName));
	const usedAliases = /* @__PURE__ */ new Set();
	const originalNameByAlias = /* @__PURE__ */ new Map();
	return {
		specs: tools.map((tool, index) => {
			let alias = tool.name;
			if (!isValidDynamicToolName(alias) || usedAliases.has(alias)) {
				const base = `ai_sdk_tool_${createHash("sha256").update(tool.name).digest("hex").slice(0, 16)}`;
				alias = base;
				let suffix = index;
				while (usedAliases.has(alias) || validOriginalNames.has(alias)) alias = `${base}_${suffix++}`;
			}
			usedAliases.add(alias);
			originalNameByAlias.set(alias, tool.name);
			return {
				type: "function",
				name: alias,
				description: tool.description ?? "",
				inputSchema: tool.inputSchema ?? {}
			};
		}),
		originalNameByAlias
	};
}
function isValidDynamicToolName(name) {
	return name.length > 0 && name.length <= 128 && /^[a-zA-Z0-9_-]+$/.test(name) && name !== "mcp" && !name.startsWith("mcp__");
}
async function handleAppServerRequest({ request, dynamicTools, emit, requestToolResult }) {
	if (request.method !== "item/tool/call") throw new Error(`Codex app-server requested unsupported method '${request.method}'.`);
	const params = asRecord(request.params);
	if (params == null || typeof params.callId !== "string" || typeof params.tool !== "string") throw new Error("Codex app-server sent an invalid dynamic tool request.");
	const originalToolName = dynamicTools.originalNameByAlias.get(params.tool);
	if (originalToolName == null) throw new Error(`Codex app-server requested unknown dynamic tool '${params.tool}'.`);
	emit({
		type: "tool-call",
		toolCallId: params.callId,
		toolName: originalToolName,
		input: JSON.stringify(params.arguments ?? {}),
		providerExecuted: false
	});
	const result = await requestToolResult(params.callId);
	emit({
		type: "tool-result",
		toolCallId: params.callId,
		toolName: originalToolName,
		result: result.output ?? null,
		isError: result.isError === true
	});
	return {
		contentItems: [{
			type: "inputText",
			text: serializeToolOutput(result.output)
		}],
		success: result.isError !== true
	};
}
function serializeToolOutput(output) {
	if (typeof output === "string") return output;
	try {
		return JSON.stringify(output) ?? String(output);
	} catch {
		return String(output);
	}
}
function readNestedString({ value, path, method }) {
	let current = value;
	for (const segment of path) current = asRecord(current)?.[segment];
	if (typeof current !== "string" || current.length === 0) throw new Error(`Codex app-server ${method} returned an invalid response.`);
	return current;
}
function assertSuccessfulTurn(result) {
	if (result.status === "completed") return;
	if (result.status === "interrupted") throw new DOMException("Codex turn was interrupted.", "AbortError");
	throw new Error(result.error ?? `Codex turn ended with status '${result.status}'.`);
}
function asRecord(value) {
	return value != null && typeof value === "object" && !Array.isArray(value) ? value : void 0;
}
//#endregion
//#region src/bridge/codex-step-tracker.ts
function createCodexStepTracker(input) {
	let stepOpen = false;
	const pendingToolItemIds = /* @__PURE__ */ new Set();
	const finishStep = () => {
		if (!stepOpen || pendingToolItemIds.size > 0) return;
		input.send({
			type: "finish-step",
			finishReason: {
				unified: "stop",
				raw: "stop"
			},
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
				if (event.type === "item.started" && itemId) pendingToolItemIds.add(itemId);
				else if (event.type === "item.completed") {
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
	return item.type === "command_execution" || item.type === "native_tool" || item.type === "mcp_tool_call" || item.type === "dynamic_tool_call" || item.type === "web_search" || item.type === "file_change" || item.type === "todo_list";
}
function defaultUsage() {
	return {
		inputTokens: {
			total: 0,
			noCache: 0,
			cacheRead: 0,
			cacheWrite: 0
		},
		outputTokens: {
			total: 0,
			text: 0
		}
	};
}
//#endregion
//#region src/bridge/create-emit-stream-event.ts
const NATIVE_TO_COMMON = {
	shell: "bash",
	web_search: "webSearch"
};
function toCommonName(nativeName) {
	return NATIVE_TO_COMMON[nativeName] ?? nativeName;
}
function getMcpToolName(item) {
	const toolName = item.tool ?? "unknown";
	return item.server != null && item.server.length > 0 ? `mcp__${item.server}__${toolName}` : toolName;
}
function createEmitStreamEvent({ send, stepTracker, setTurnUsage, setThreadId, emitWarning, emitError }) {
	const textByItem = /* @__PURE__ */ new Map();
	const reasoningByItem = /* @__PURE__ */ new Map();
	const emittedWebSearchToolCalls = /* @__PURE__ */ new Set();
	return (event) => {
		if (event.type === "thread.started" && typeof event.thread_id === "string") {
			setThreadId(event.thread_id);
			send({
				type: "bridge-thread",
				threadId: event.thread_id
			});
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
		const id = item.id ?? randomUUID();
		const observeStep = () => {
			stepTracker.observeEvent({
				event,
				itemId: id
			});
		};
		if (item.type === "native_tool" && item.tool != null) {
			if (event.type === "item.started" && item.input != null) send({
				type: "tool-call",
				toolCallId: id,
				toolName: item.tool,
				input: item.input,
				providerExecuted: true
			});
			else if (event.type === "item.completed") send({
				type: "tool-result",
				toolCallId: id,
				toolName: item.tool,
				result: item.result,
				...item.isError ? { isError: true } : {}
			});
			stepTracker.observeEvent({
				event,
				itemId: `native-tool:${id}`
			});
			return;
		}
		if (item.type === "agent_message" && typeof item.text === "string") {
			if (!textByItem.has(id)) {
				send({
					type: "text-start",
					id
				});
				textByItem.set(id, "");
			}
			const last = textByItem.get(id) ?? "";
			const next = item.text;
			if (next.length > last.length) {
				send({
					type: "text-delta",
					id,
					delta: next.slice(last.length)
				});
				textByItem.set(id, next);
			}
			if (event.type === "item.completed") send({
				type: "text-end",
				id
			});
			observeStep();
			return;
		}
		if (item.type === "reasoning" && typeof item.text === "string") {
			if (!reasoningByItem.has(id)) {
				send({
					type: "reasoning-start",
					id
				});
				reasoningByItem.set(id, "");
			}
			const last = reasoningByItem.get(id) ?? "";
			const next = item.text;
			if (next.length > last.length) {
				send({
					type: "reasoning-delta",
					id,
					delta: next.slice(last.length)
				});
				reasoningByItem.set(id, next);
			}
			if (event.type === "item.completed") send({
				type: "reasoning-end",
				id
			});
			observeStep();
			return;
		}
		if (item.type === "command_execution") {
			const nativeName = "shell";
			if (event.type === "item.started") send({
				type: "tool-call",
				toolCallId: id,
				toolName: toCommonName(nativeName),
				nativeName,
				input: JSON.stringify({ command: item.command ?? "" }),
				providerExecuted: true
			});
			else if (event.type === "item.completed") send({
				type: "tool-result",
				toolCallId: id,
				toolName: toCommonName(nativeName),
				result: {
					exitCode: item.exit_code ?? null,
					output: item.aggregated_output ?? "",
					status: item.status ?? "completed"
				}
			});
			observeStep();
			return;
		}
		if (item.type === "mcp_tool_call") {
			const toolName = getMcpToolName(item);
			if (event.type === "item.started") send({
				type: "tool-call",
				toolCallId: id,
				toolName,
				nativeName: toolName,
				input: JSON.stringify(item.arguments ?? {}),
				providerExecuted: true,
				dynamic: true
			});
			else if (event.type === "item.completed") send({
				type: "tool-result",
				toolCallId: id,
				toolName,
				result: extractMcpToolCallResult(item),
				dynamic: true
			});
			observeStep();
			return;
		}
		if (item.type === "web_search") {
			const nativeName = "web_search";
			const query = getWebSearchQuery(item);
			if (event.type === "item.started" || event.type === "item.updated") {
				if (query !== void 0) emitWebSearchToolCall({
					id,
					query,
					nativeName,
					emittedWebSearchToolCalls,
					send
				});
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
			for (const change of item.changes ?? []) send({
				type: "file-change",
				event: change.kind === "add" ? "create" : change.kind === "delete" ? "delete" : "modify",
				path: change.path
			});
			observeStep();
			return;
		}
		if (item.type === "error" && event.type === "item.completed") emitWarning({ message: typeof item.message === "string" && item.message.trim() ? item.message : "codex reported a non-fatal error item" });
	};
}
function getWebSearchQuery(item) {
	if (typeof item.query === "string" && item.query.length > 0) return item.query;
	if (typeof item.action?.query === "string" && item.action.query.length > 0) return item.action.query;
}
function emitWebSearchToolCall({ id, query, nativeName, emittedWebSearchToolCalls, send }) {
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
	if (item.result === void 0 || item.result === null || typeof item.result !== "object") return item.error?.message ? { error: item.error.message } : null;
	const result = item.result;
	if (result.structured_content !== void 0 && result.structured_content !== null) return result.structured_content;
	return result.content ?? null;
}
function mapUsage(usage) {
	const input = usage.input_tokens ?? 0;
	const cacheRead = usage.cached_input_tokens ?? 0;
	const cacheWrite = usage.cache_write_input_tokens ?? 0;
	return {
		inputTokens: {
			total: input,
			noCache: Math.max(0, input - cacheRead - cacheWrite),
			cacheRead,
			cacheWrite
		},
		outputTokens: {
			total: usage.output_tokens ?? 0,
			text: usage.output_tokens ?? 0
		}
	};
}
//#endregion
//#region src/bridge/index.ts
const args = parseArgs(argv.slice(2));
const workdir = requireArg({
	value: args.workdir,
	name: "--workdir"
});
const bridgeStateDir = requireArg({
	value: args.bridgeStateDir,
	name: "--bridge-state-dir"
});
const HARNESS_CLIENT_APP = env.AI_SDK_HARNESS_CLIENT_APP;
const threadState = { id: void 0 };
const appServer = createCodexAppServerRuntime();
await runBridge({
	bridgeType: "codex",
	bridgeStateDir,
	onStart: runTurn,
	onStop: async () => {
		const data = threadState.id ? { threadId: threadState.id } : {};
		await appServer.close();
		return data;
	},
	onDestroy: () => appServer.close()
});
async function runTurn(start, turn) {
	const emit = (msg) => turn.emit(msg);
	if (start.restartThread) threadState.id = void 0;
	else if (typeof start.resumeThreadId === "string" && start.resumeThreadId.length > 0) threadState.id = start.resumeThreadId;
	const runtime = resolveCodexRuntime({ start });
	let turnUsage = defaultUsage();
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
		await appServer.runTurn({
			start,
			turn,
			emit,
			workdir,
			threadId: threadState.id,
			codexModel: runtime.codexModel,
			codexConfig: runtime.codexConfig,
			stepTracker,
			emitStreamEvent
		});
	} catch (error) {
		if (!turn.abortSignal.aborted) turn.emitError({
			error,
			message: "codex turn failed"
		});
		return;
	}
	emit({
		type: "finish",
		finishReason: {
			unified: "stop",
			raw: "stop"
		},
		totalUsage: turnUsage
	});
}
function resolveCodexRuntime({ start }) {
	const codexConfig = {
		...start.codexConfig,
		developer_instructions: [start.instructions, "Only respond with your `final` message once you have fully addressed the user request."].filter((instruction) => Boolean(instruction)).join("\n\n"),
		model_reasoning_summary: "detailed"
	};
	const gatewayBaseUrl = env.AI_GATEWAY_BASE_URL;
	const hasGatewayAuth = Boolean(env.AI_GATEWAY_API_KEY || gatewayBaseUrl);
	if (hasGatewayAuth && !gatewayBaseUrl) throw new Error("AI Gateway auth was selected but AI_GATEWAY_BASE_URL is missing from the Codex bridge environment.");
	const apiBaseUrl = hasGatewayAuth ? gatewayBaseUrl : env.OPENAI_BASE_URL ?? (env.CODEX_API_KEY != null || start.headers != null ? "https://api.openai.com/v1" : void 0);
	const codexModel = start.model && hasGatewayAuth && !start.model.includes("/") ? `openai/${start.model}` : start.model;
	if (hasGatewayAuth && codexModel?.startsWith("openai/")) codexConfig.model_supports_reasoning_summaries = true;
	if (apiBaseUrl) {
		codexConfig.preferred_auth_method = "apikey";
		codexConfig.model_provider = "agent_bridge_openai";
		codexConfig.model_providers = { agent_bridge_openai: {
			name: env.CODEX_MODEL_PROVIDER_NAME || "Agent Bridge OpenAI",
			base_url: apiBaseUrl,
			env_key: "CODEX_API_KEY",
			wire_api: "responses",
			supports_websockets: false,
			...start.headers != null || hasGatewayAuth && HARNESS_CLIENT_APP ? { http_headers: {
				...start.headers,
				...hasGatewayAuth && HARNESS_CLIENT_APP ? {
					"User-Agent": HARNESS_CLIENT_APP,
					"x-client-app": HARNESS_CLIENT_APP
				} : {}
			} } : {}
		} };
	}
	if (start.mcpServers != null) codexConfig.mcp_servers = start.mcpServers;
	return {
		codexConfig,
		codexModel
	};
}
function parseArgs(args) {
	const out = {};
	for (let i = 0; i < args.length; i++) if (args[i] === "--workdir" && i + 1 < args.length) out.workdir = args[++i];
	else if (args[i] === "--bridge-state-dir" && i + 1 < args.length) out.bridgeStateDir = args[++i];
	return out;
}
function emitFatal(message) {
	stdout.write(JSON.stringify({
		type: "bridge-fatal",
		message
	}) + "\n");
	process.exit(1);
}
function requireArg({ value, name }) {
	if (!value) emitFatal(`Missing ${name} argument.`);
	return value;
}
//#endregion
export {};

//# sourceMappingURL=index.mjs.map